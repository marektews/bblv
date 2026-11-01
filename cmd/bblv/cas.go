package main

import (
	"path/filepath"

	"github.com/marektews/bblv/internal/trust"
)

// printerCAFile keeps, in the data directory, the CAs of the printers learned
// on their first connection, with the printers pinned to them.
const printerCAFile = "printer-ca.pem"

// printerCAs loads the printer CAs of the data directory, so that the
// commands talking to a printer check its certificate like the server does
// and send the access code to no other device.
func printerCAs(dataDir string) (*trust.Store, error) {
	return trust.Load(filepath.Join(dataDir, printerCAFile))
}
