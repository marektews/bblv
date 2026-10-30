// Package printer talks to a Bambu Lab printer over its local MQTT broker.
package printer

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"

	"github.com/marektews/bblv/internal/config"
)

const (
	pushAll    = `{"pushing":{"sequence_id":"0","command":"pushall","version":1,"push_target":1}}`
	getVersion = `{"info":{"sequence_id":"0","command":"get_version"}}`
)

// Report is a raw message received from the printer.
type Report struct {
	Time    time.Time
	Payload []byte
}

// Client keeps an MQTT connection to one printer and the state merged from its reports.
type Client struct {
	p            config.Printer
	handler      func(Report)
	onConnection func(connected bool)
	conn         mqtt.Client
	state        *State
}

// New creates a client. handler is called for every report after it has been
// merged into the state; calls are sequential and must not block for long.
func New(p config.Printer, handler func(Report)) *Client {
	c := &Client{p: p, handler: handler, state: NewState()}
	opts := mqtt.NewClientOptions().
		AddBroker("ssl://" + p.Host + ":8883").
		SetClientID("bblv-" + randomID()).
		SetUsername("bblp").
		SetPassword(p.AccessCode).
		// The printer presents a certificate issued by Bambu's private CA for
		// its serial number, so there is no public chain to verify against.
		SetTLSConfig(&tls.Config{InsecureSkipVerify: true}).
		SetKeepAlive(30 * time.Second).
		SetAutoReconnect(true).
		SetMaxReconnectInterval(30 * time.Second).
		SetOnConnectHandler(c.onConnect).
		SetConnectionLostHandler(func(_ mqtt.Client, err error) {
			slog.Warn("printer connection lost", "printer", p.ID, "err", err)
			if c.onConnection != nil {
				c.onConnection(false)
			}
		})
	c.conn = mqtt.NewClient(opts)
	return c
}

// OnConnection sets a function called when the connection is established or
// lost. It must be set before Connect.
func (c *Client) OnConnection(f func(connected bool)) {
	c.onConnection = f
}

// Connect establishes the first connection. Later drops are reconnected automatically.
func (c *Client) Connect(ctx context.Context) error {
	t := c.conn.Connect()
	select {
	case <-t.Done():
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := t.Error(); err != nil {
		return fmt.Errorf("connect to %s: %w (%s)", c.p.Host, err, connectHint(err))
	}
	return nil
}

// connectHint tells what to check after a failed connection.
func connectHint(err error) string {
	if errors.Is(err, packets.ErrorRefusedBadUsernameOrPassword) || errors.Is(err, packets.ErrorRefusedNotAuthorised) {
		return "check the access code"
	}
	return "check that the printer is on and its address is right; TCP port 8883 must be reachable"
}

// Close disconnects from the printer.
func (c *Client) Close() {
	c.conn.Disconnect(250)
}

// Status returns a typed snapshot of the current state.
func (c *Client) Status() Status {
	return c.state.Status()
}

func (c *Client) onConnect(conn mqtt.Client) {
	slog.Info("printer connected", "printer", c.p.ID, "host", c.p.Host)
	if c.onConnection != nil {
		c.onConnection(true)
	}
	report := "device/" + c.p.Serial + "/report"
	if t := conn.Subscribe(report, 0, c.onMessage); !t.WaitTimeout(10*time.Second) || t.Error() != nil {
		slog.Error("subscribe failed", "printer", c.p.ID, "err", t.Error())
		return
	}
	request := "device/" + c.p.Serial + "/request"
	for _, cmd := range []string{getVersion, pushAll} {
		if t := conn.Publish(request, 0, false, cmd); !t.WaitTimeout(10*time.Second) || t.Error() != nil {
			slog.Error("request failed", "printer", c.p.ID, "err", t.Error())
		}
	}
}

func (c *Client) onMessage(_ mqtt.Client, m mqtt.Message) {
	r := Report{Time: time.Now(), Payload: m.Payload()}
	if err := c.state.Apply(r.Payload); err != nil {
		slog.Warn("invalid report", "printer", c.p.ID, "err", err)
	}
	if c.handler != nil {
		c.handler(r)
	}
}

func randomID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
