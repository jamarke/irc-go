// Copyright 2009 Thomas Jager <mail@jager.no>  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ircevent

import (
	"crypto/tls"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/goshuirc/irc-go/ircmsg"
)

type empty struct{}

type Callback func(Event)

type capResult struct {
	capName string
	ack     bool
}

type Connection struct {
	// config data, user-settable
	Server        string
	TLSConfig     *tls.Config
	Nick          string
	User          string
	RealName      string   // IRC realname/gecos
	WebIRC        []string // parameters for the WEBIRC command
	Password      string   // server password (PASS command)
	RequestCaps   []string // IRCv3 capabilities to request (failure is non-fatal)
	SASLLogin     string   // SASL credentials to log in with (failure is fatal)
	SASLPassword  string
	SASLMech      string
	QuitMessage   string
	Version       string
	Timeout       time.Duration
	KeepAlive     time.Duration
	ReconnectFreq time.Duration
	MaxLineLen    int // maximum line length, not including tags
	UseTLS        bool
	UseSASL       bool
	EnableCTCP    bool
	Debug         bool
	AllowPanic    bool // if set, don't recover() from panics in callbacks

	// networking and synchronization
	stateMutex sync.Mutex     // innermost mutex: don't block while holding this
	end        chan empty     // closing this causes the goroutines to exit (think threading.Event)
	pwrite     chan []byte    // receives IRC lines to be sent to the socket
	wg         sync.WaitGroup // after closing end, wait on this for all the goroutines to stop
	socket     net.Conn
	lastError  error
	quitAt     time.Time // time Quit() was called
	running    bool      // is a connection active? is `end` open?
	quit       bool      // user called Quit, do not reconnect
	pingSent   bool      // we sent PING and are waiting for PONG

	// IRC protocol connection state
	currentNick      string // nickname assigned by the server, empty before registration
	acknowledgedCaps []string
	nickCounter      int
	// Connect() builds these with sufficient capacity to receive all expected
	// responses during negotiation. Sends to them are nonblocking, so anything
	// sent outside of negotiation will not cause the relevant callbacks to block.
	welcomeChan chan empty      // signals that we got 001 and we are now connected
	saslChan    chan saslResult // transmits the final outcome of SASL negotiation
	capsChan    chan capResult  // transmits the final status of each CAP negotiated

	// callback state
	eventsMutex      sync.Mutex
	events           map[string]map[uint64]Callback
	idCounter        uint64 // assign unique IDs to callbacks
	hasBaseCallbacks bool

	Log *log.Logger
}

// A struct to represent an event.
type Event struct {
	ircmsg.IRCMessage
}

// Retrieve the last message from Event arguments.
// This function leaves the arguments untouched and
// returns an empty string if there are none.
func (e *Event) Message() string {
	if len(e.Params) == 0 {
		return ""
	}
	return e.Params[len(e.Params)-1]
}

/*
// https://stackoverflow.com/a/10567935/6754440
// Regex of IRC formatting.
var ircFormat = regexp.MustCompile(`[\x02\x1F\x0F\x16\x1D\x1E]|\x03(\d\d?(,\d\d?)?)?`)

// Retrieve the last message from Event arguments, but without IRC formatting (color.
// This function leaves the arguments untouched and
// returns an empty string if there are none.
func (e *Event) MessageWithoutFormat() string {
	if len(e.Arguments) == 0 {
		return ""
	}
	return ircFormat.ReplaceAllString(e.Arguments[len(e.Arguments)-1], "")
}
*/

func (e *Event) Nick() string {
	nick, _, _ := e.splitNUH()
	return nick
}

func (e *Event) User() string {
	_, user, _ := e.splitNUH()
	return user
}

func (e *Event) Host() string {
	_, _, host := e.splitNUH()
	return host
}

func (event *Event) splitNUH() (nick, user, host string) {
	if i, j := strings.Index(event.Prefix, "!"), strings.Index(event.Prefix, "@"); i > -1 && j > -1 && i < j {
		nick = event.Prefix[0:i]
		user = event.Prefix[i+1 : j]
		host = event.Prefix[j+1:]
	}
	return
}
