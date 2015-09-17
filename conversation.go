package otr3

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"log"
)

type msgState int

const (
	plainText msgState = iota
	encrypted
	finished
)

var (
	queryMarker = []byte("?OTR")
	errorMarker = []byte("?OTR Error:")
	msgMarker   = []byte("?OTR:")
)

// Conversation contains all the information for a specific connection between two peers in an IM system.
// Policies are not supposed to change once a conversation has been used
type Conversation struct {
	version otrVersion
	Rand    io.Reader

	msgState        msgState
	whitespaceState whitespaceState

	ourInstanceTag   uint32
	theirInstanceTag uint32

	ssid     [8]byte
	ourKey   *PrivateKey
	theirKey *PublicKey

	ake        *ake
	smp        smp
	keys       keyManagementContext
	Policies   policies
	heartbeat  heartbeatContext
	resend     resendContext
	injections injections

	fragmentSize         uint16
	fragmentationContext fragmentationContext

	smpEventHandler      SMPEventHandler
	errorMessageHandler  ErrorMessageHandler
	messageEventHandler  MessageEventHandler
	securityEventHandler SecurityEventHandler
	receivedKeyHandler   ReceivedKeyHandler

	debug         bool
	sentRevealSig bool
}

func (c *Conversation) messageHeader(msgType byte) ([]byte, error) {
	return c.version.messageHeader(c, msgType)
}

func (c *Conversation) parseMessageHeader(msg messageWithHeader) ([]byte, []byte, error) {
	return c.version.parseMessageHeader(c, msg)
}

func (c *Conversation) resolveVersionFromFragment(fragment []byte) error {
	versions := 1 << versionFromFragment(fragment)
	return c.commitToVersionFrom(versions)
}

func (c *Conversation) parseFragmentPrefix(data []byte) ([]byte, bool, bool) {
	if err := c.resolveVersionFromFragment(data); err != nil {
		return data, true, false
	}

	return c.version.parseFragmentPrefix(c, data)
}

func (c *Conversation) wrapMessageHeader(msgType byte, msg []byte) (messageWithHeader, error) {
	header, err := c.messageHeader(msgType)
	if err != nil {
		return nil, err
	}

	return append(header, msg...), nil
}

// IsEncrypted returns true if the current conversation is private
func (c *Conversation) IsEncrypted() bool {
	return c.msgState == encrypted
}

// End ends a secure conversation by generating a termination message for
// the peer and switches to unencrypted communication.
func (c *Conversation) End() (toSend []ValidMessage, err error) {
	previousMsgState := c.msgState
	if c.msgState == encrypted {
		c.smp.wipe()
		// Error can only happen when Rand reader is broken
		toSend, _, err = c.createSerializedDataMessage(nil, messageFlagIgnoreUnreadable, []tlv{tlv{tlvType: tlvTypeDisconnected}})
	}
	c.msgState = plainText
	defer c.signalSecurityEventIf(previousMsgState == encrypted, GoneInsecure)

	c.keys.ourCurrentDHKeys.wipe()
	c.keys.ourPreviousDHKeys.wipe()
	wipeBigInt(c.keys.theirCurrentDHPubKey)
	return
}

// SetKeys assigns ourKey (private) and theirKey (public) to the Conversation
func (c *Conversation) SetKeys(ourKey *PrivateKey, theirKey *PublicKey) {
	c.ourKey = ourKey
	c.theirKey = theirKey
}

// GetTheirKey returns the public key of the other peer in this conversation
func (c *Conversation) GetTheirKey() *PublicKey {
	return c.theirKey
}

// GetSSID returns the SSID of this Conversation
func (c *Conversation) GetSSID() [8]byte {
	return c.ssid
}

// SetSMPEventHandler assigns handler for SMPEvent
func (c *Conversation) SetSMPEventHandler(handler SMPEventHandler) {
	c.smpEventHandler = handler
}

// SetErrorMessageHandler assigns handler for ErrorMessage
func (c *Conversation) SetErrorMessageHandler(handler ErrorMessageHandler) {
	c.errorMessageHandler = handler
}

// SetMessageEventHandler assigns handler for MessageEvent
func (c *Conversation) SetMessageEventHandler(handler MessageEventHandler) {
	c.messageEventHandler = handler
}

// SetSecurityEventHandler assigns handler for SecurityEvent
func (c *Conversation) SetSecurityEventHandler(handler SecurityEventHandler) {
	c.securityEventHandler = handler
}

func (c Conversation) GobEncode() ([]byte, error) {
	w := new(bytes.Buffer)
	encoder := gob.NewEncoder(w)
	err := encoder.Encode(&c.version)
	if err != nil {
		return nil, err
	}
	err = encoder.Encode(&c.ake)
	if err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func (c *Conversation) GobDecode(buf []byte) error {
	r := bytes.NewBuffer(buf)
	decoder := gob.NewDecoder(r)
	err := decoder.Decode(&c.version)
	if err != nil {
		return err
	}
	return decoder.Decode(&c.ake)
}

func (c *Conversation) ToGob(buf *bytes.Buffer) {
	gob.Register(otrV3{})

	// Create an encoder and send a value.
	enc := gob.NewEncoder(buf)
	err := enc.Encode(c)
	if err != nil {
		log.Fatal("encode:", err)
	}

	// Create a decoder and receive a value.
	dec := gob.NewDecoder(buf)
	var v Conversation
	err = dec.Decode(&v)
	// if err != nil {
	// 	log.Fatal("decode:", err)
	// }
	fmt.Println(v.version.protocolVersion())
	fmt.Println(v.ake)
}
