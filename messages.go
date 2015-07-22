package otr3

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
)

type message interface {
	serialize() []byte
	deserialize(msg []byte) error
}

type dhMessage struct {
	protocolVersion     uint16
	needInstanceTag     bool
	senderInstanceTag   uint32
	receiverInstanceTag uint32
}

type dhCommit struct {
	dhMessage
	gx          *big.Int
	encryptedGx []byte
	hashedGx    [sha256.Size]byte
}

func (c *dhCommit) serialize() []byte {
	out := appendShort(nil, c.protocolVersion)
	out = append(out, msgTypeDHCommit)
	if c.needInstanceTag {
		out = appendWord(out, c.senderInstanceTag)
		out = appendWord(out, c.receiverInstanceTag)
	}
	out = appendData(out, c.encryptedGx)
	if c.hashedGx == [sha256.Size]byte{} {
		c.hashedGx = sha256Sum(appendMPI(nil, c.gx))
	}
	out = appendData(out, c.hashedGx[:])

	return out
}

func (c *dhCommit) deserialize(msg []byte) error {
	var ok1 bool
	msg, c.encryptedGx, ok1 = extractData(msg)
	_, h, ok2 := extractData(msg)
	if !ok1 || !ok2 {
		return errors.New("otr: corrupt DH commit message")
	}
	copy(c.hashedGx[:], h)
	return nil
}

type dhKey struct {
	dhMessage
	gy *big.Int
}

func (c *dhKey) serialize() []byte {
	out := appendShort(nil, c.protocolVersion)
	out = append(out, msgTypeDHKey)
	if c.needInstanceTag {
		out = appendWord(out, c.senderInstanceTag)
		out = appendWord(out, c.receiverInstanceTag)
	}

	return appendMPI(out, c.gy)
}

func (c *dhKey) deserialize(msg []byte) error {
	_, gy, ok := extractMPI(msg)

	if !ok {
		return errors.New("otr: corrupt DH key message")
	}

	// TODO: is this only for otrv3 or for v2 too?
	if lt(gy, g1) || gt(gy, pMinusTwo) {
		return errors.New("otr: DH value out of range")
	}

	c.gy = gy
	return nil
}

type revealSig struct {
	dhMessage
	r            [16]byte
	encryptedSig []byte
	macSig       []byte
}

func (c *revealSig) serialize() []byte {
	out := appendShort(nil, c.protocolVersion)
	out = append(out, msgTypeRevealSig)
	if c.needInstanceTag {
		out = appendWord(out, c.senderInstanceTag)
		out = appendWord(out, c.receiverInstanceTag)
	}
	out = appendData(out, c.r[:])
	out = append(out, c.encryptedSig...)
	return append(out, c.macSig[:20]...)
}

func (c *revealSig) deserialize(msg []byte) error {
	in, r, ok1 := extractData(msg)
	macSig, encryptedSig, ok2 := extractData(in)
	if !ok1 || !ok2 || len(macSig) != 20 {
		return errors.New("otr: corrupt reveal signature message")
	}

	copy(c.r[:], r)
	c.encryptedSig = encryptedSig
	c.macSig = macSig
	return nil
}

type sig struct {
	dhMessage
	encryptedSig []byte
	macSig       []byte
}

func (c *sig) serialize() []byte {
	out := appendShort(nil, c.protocolVersion)
	out = append(out, msgTypeSig)
	if c.needInstanceTag {
		out = appendWord(out, c.senderInstanceTag)
		out = appendWord(out, c.receiverInstanceTag)
	}
	out = append(out, c.encryptedSig...)
	return append(out, c.macSig[:20]...)
}

func (c *sig) deserialize(msg []byte) error {
	macSig, encryptedSig, ok := extractData(msg)

	if !ok || len(macSig) != 20 {
		return errors.New("otr: corrupt signature message")
	}
	c.encryptedSig = encryptedSig
	c.macSig = macSig
	return nil
}

type dataMsg struct {
	nul  byte
	tlvs []tlv
}

type tlv struct {
	tlvType   uint16
	tlvLength uint16
	tlvValue  []byte
}

func (c *dataMsg) deserialize(msg []byte) error {
	if msg[0] != 0x00 {
		return errors.New("otr: corrupt data message")
	}
	c.nul = msg[0]
	tlvsBytes := msg[1:]
	index := 0
	for index < len(tlvsBytes) {
		atlv := tlv{}
		atlv.tlvType = binary.BigEndian.Uint16(tlvsBytes[index : index+2])
		atlv.tlvLength = binary.BigEndian.Uint16(tlvsBytes[index+2 : index+4])
		endOfTLV := index + 4 + int(atlv.tlvLength)
		if endOfTLV > len(tlvsBytes) {
			return errors.New("otr: wrong tlv length")
		}
		atlv.tlvValue = tlvsBytes[index+4 : endOfTLV]
		c.tlvs = append(c.tlvs, atlv)
		index = endOfTLV
	}
	return nil
}

func (c *dataMsg) serialize() []byte {
	out := []byte{0x00}
	for i := range c.tlvs {
		out = appendShort(out, c.tlvs[i].tlvType)
		out = appendShort(out, c.tlvs[i].tlvLength)
		out = append(out, c.tlvs[i].tlvValue...)
	}
	return out
}