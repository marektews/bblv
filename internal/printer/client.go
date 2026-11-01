// Package printer talks to a Bambu Lab printer over its local MQTT broker.
package printer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"

	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/trust"
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

// New creates a client checking the printer's certificate against cas, any
// certificate when nil. handler is called for every report after it has been
// merged into the state; calls are sequential and must not block for long.
func New(p config.Printer, cas *trust.Store, handler func(Report)) *Client {
	c := &Client{p: p, handler: handler, state: NewState()}
	opts := mqtt.NewClientOptions().
		AddBroker("ssl://" + net.JoinHostPort(p.Host, "8883")). // brackets an IPv6 address
		SetClientID("bblv-" + randomID()).
		SetUsername("bblp").
		SetPassword(p.AccessCode).
		// The printer presents a certificate issued by Bambu's private CA for
		// its serial number; cas knows the CA after the first connection.
		SetTLSConfig(cas.TLS(p.Serial)).
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
		if hint := connectHint(err); hint != "" {
			return fmt.Errorf("connect to %s: %w (%s)", c.p.Host, err, hint)
		}
		return fmt.Errorf("connect to %s: %w", c.p.Host, err)
	}
	return nil
}

// connectHint tells what to check after a failed connection, "" when the
// error says it itself.
func connectHint(err error) string {
	switch {
	case errors.Is(err, packets.ErrorRefusedBadUsernameOrPassword) || errors.Is(err, packets.ErrorRefusedNotAuthorised):
		return "check the access code"
	case errors.Is(err, trust.ErrUntrusted) || errors.As(err, new(*trust.FileError)):
		return ""
	}
	return "check that the printer is on and its address is right; TCP port 8883 must be reachable"
}

// Close disconnects from the printer.
func (c *Client) Close() {
	c.conn.Disconnect(250)
}

// ErrNotConnected is returned for a command while the printer is offline.
var ErrNotConnected = errors.New("the printer is not connected")

// SetLight switches the chamber light on or off. The printer answers with
// "result":"success" and reports the new mode in lights_report; on an A1 in
// cloud mode it accepts the command without LAN-only or developer mode.
func (c *Client) SetLight(on bool) error {
	if !c.conn.IsConnectionOpen() {
		return ErrNotConnected
	}
	t := c.conn.Publish("device/"+c.p.Serial+"/request", 0, false, lightCommand(on))
	if !t.WaitTimeout(5 * time.Second) {
		return errors.New("the printer did not take the command in time")
	}
	return t.Error()
}

func lightCommand(on bool) string {
	mode := "off"
	if on {
		mode = "on"
	}
	return `{"system":{"sequence_id":"0","command":"ledctrl","led_node":"chamber_light","led_mode":"` + mode +
		`","led_on_time":500,"led_off_time":500,"loop_times":0,"interval_time":0}}`
}

// Status returns a typed snapshot of the current state.
func (c *Client) Status() Status {
	return c.state.Status()
}

// onConnect runs in a goroutine of its own after every (re)connection.
func (c *Client) onConnect(conn mqtt.Client) {
	slog.Info("printer connected", "printer", c.p.ID, "host", c.p.Host)
	// The printer is online only once its reports can arrive; before, the
	// view would show the last known state as current.
	report := "device/" + c.p.Serial + "/report"
	if err := subscribe(conn, report, c.onMessage); errors.Is(err, ErrNotConnected) {
		return // lost again; the next connection subscribes
	} else if err != nil {
		// A late answer still lets the reports in, which then mark the
		// printer online; the requests below are harmless anyway.
		slog.Error("subscribe failed", "printer", c.p.ID, "err", err)
	} else if c.onConnection != nil {
		c.onConnection(true)
	}
	request := "device/" + c.p.Serial + "/request"
	for _, cmd := range []string{getVersion, pushAll} {
		if t := conn.Publish(request, 0, false, cmd); !t.WaitTimeout(10*time.Second) || t.Error() != nil {
			slog.Error("request failed", "printer", c.p.ID, "err", t.Error())
		}
	}
}

// subscribeTimeout is how long to wait for the printer to confirm a
// subscription.
var subscribeTimeout = 10 * time.Second

// subackFailure is the return code of a refused subscription (MQTT 3.1.1).
const subackFailure = 0x80

// subscribe subscribes to topic, trying again a few times while connected.
// paho reports a broker refusing the subscription only in the result of the
// token, not as its error.
func subscribe(conn mqtt.Client, topic string, handler mqtt.MessageHandler) error {
	err := ErrNotConnected
	for range 3 {
		if !conn.IsConnectionOpen() {
			return ErrNotConnected
		}
		t := conn.Subscribe(topic, 0, handler)
		switch {
		case !t.WaitTimeout(subscribeTimeout):
			err = errors.New("the printer did not confirm the subscription")
		case t.Error() != nil:
			err = t.Error()
		default:
			if st, ok := t.(*mqtt.SubscribeToken); ok && st.Result()[topic] == subackFailure {
				err = errors.New("the printer refused the subscription")
				continue
			}
			return nil
		}
	}
	return err
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
