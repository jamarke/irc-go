// written by Daniel Oaks <daniel@danieloaks.net>
// released under the ISC license

package gircclient

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/DanielOaks/girc-go/eventmgr"
	"github.com/DanielOaks/girc-go/ircmsg"
)

// ServerConnection is a connection to a single server.
type ServerConnection struct {
	Name       string
	Connected  bool
	Registered bool

	// internal stuff
	connection net.Conn
	eventsIn   eventmgr.EventManager
	eventsOut  eventmgr.EventManager

	// data we keep track of
	// Features ServerFeatures
	Caps ClientCapabilities

	// details users must supply before connection
	Nick            string
	InitialNick     string
	InitialUser     string
	InitialRealName string
}

// newServerConnection returns an initialised ServerConnection, for internal
// use.
func newServerConnection(name string) *ServerConnection {
	var sc ServerConnection

	sc.Name = name
	sc.Caps = NewClientCapabilities()

	sc.Caps.AddWantedCaps("account-notify", "away-notify", "extended-join", "multi-prefix", "sasl")
	sc.Caps.AddWantedCaps("account-tag", "chghost", "echo-message", "invite-notify", "server-time", "userhost-in-names")

	return &sc
}

// Connect connects to the given address.
func (sc *ServerConnection) Connect(address string, ssl bool, tlsconfig *tls.Config) error {
	// check the required attributes
	if sc.InitialNick == "" || sc.InitialUser == "" {
		return errors.New("InitialNick and InitialUser must be set before connecting")
	}

	// connect
	var conn net.Conn
	var err error

	if ssl {
		conn, err = tls.Dial("tcp", address, tlsconfig)
	} else {
		conn, err = net.Dial("tcp", address)
	}

	if err != nil {
		return err
	}

	sc.connection = conn
	sc.Connected = true

	sc.Send(nil, "", "CAP", "LS", "302")

	return nil
}

// ReceiveLoop runs a loop of receiving and dispatching new messages.
func (sc *ServerConnection) ReceiveLoop() {
	// wait for the connection to become available
	for sc.connection == nil {
		waitTime, _ := time.ParseDuration("10ms")
		time.Sleep(waitTime)
	}

	reader := bufio.NewReader(sc.connection)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			sc.Connected = false
			break
		}
		line = strings.Trim(line, "\r\n")

		// ignore empty lines
		if len(line) < 1 {
			continue
		}

		// dispatch raw
		rawInfo := eventmgr.NewInfoMap()
		rawInfo["server"] = sc
		rawInfo["direction"] = "in"
		rawInfo["data"] = line

		sc.dispatchRawIn(rawInfo)

		// dispatch events
		message, err := ircmsg.ParseLine(line)
		info := eventmgr.NewInfoMap()
		info["server"] = sc
		info["tags"] = message.Tags
		info["prefix"] = message.Prefix
		info["command"] = message.Command
		info["params"] = message.Params

		// IRC commands are case-insensitive
		sc.dispatchIn(strings.ToUpper(message.Command), info)
	}

	sc.connection.Close()
}

// RegisterEvent registers a new handler for the given event.
//
// The standard directions are "in" and "out".
//
// 'name' can either be the name of an event, "all", or "raw". Note that "all"
// will not catch "raw" events, but will catch all others.
func (sc *ServerConnection) RegisterEvent(direction string, name string, handler eventmgr.HandlerFn, priority int) {
	if direction == "in" || direction == "both" {
		sc.eventsIn.Attach(name, handler, priority)
	}
	if direction == "out" || direction == "both" {
		sc.eventsOut.Attach(name, handler, priority)
	}
}

// Shutdown closes the connection to the server.
func (sc *ServerConnection) Shutdown(message string) {
	sc.Send(nil, "", "QUIT", message)
	sc.Connected = false
	sc.connection.Close()
}

// Send sends an IRC message to the server.
func (sc *ServerConnection) Send(tags *map[string]ircmsg.TagValue, prefix string, command string, params ...string) {
	ircmsg := ircmsg.MakeMessage(tags, prefix, command, params...)
	line, err := ircmsg.Line()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	fmt.Fprintf(sc.connection, line)

	// dispatch raw event
	info := eventmgr.NewInfoMap()
	info["server"] = sc
	info["direction"] = "out"
	info["data"] = line
	sc.dispatchRawOut(info)
}

// dispatchRawIn dispatches raw inbound messages.
func (sc *ServerConnection) dispatchRawIn(info eventmgr.InfoMap) {
	sc.eventsIn.Dispatch("raw", info)
}

// dispatchIn dispatches inbound messages.
func (sc *ServerConnection) dispatchIn(name string, info eventmgr.InfoMap) {
	sc.eventsIn.Dispatch(name, info)
	sc.eventsIn.Dispatch("all", info)
}

// dispatchRawOut dispatches raw outbound messages.
func (sc *ServerConnection) dispatchRawOut(info eventmgr.InfoMap) {
	sc.eventsOut.Dispatch("raw", info)
}

// dispatchOut dispatches outbound messages.
func (sc *ServerConnection) dispatchOut(name string, info eventmgr.InfoMap) {
	sc.eventsOut.Dispatch(name, info)
	sc.eventsOut.Dispatch("all", info)
}
