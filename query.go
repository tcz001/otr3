package otr3

import (
	"bytes"
	"strconv"
)

func isQueryMessage(msg []byte) bool {
	return bytes.HasPrefix(msg, queryMarker)
}

func parseOTRQueryMessage(msg []byte) []int {
	ret := []int{}

	if bytes.HasPrefix(msg, queryMarker) && len(msg) > len(queryMarker) {
		versions := msg[len(queryMarker):]

		if versions[0] == '?' {
			ret = append(ret, 1)
			versions = versions[1:]
		}

		if len(versions) > 0 && versions[0] == 'v' {
			for _, c := range versions {
				if v, err := strconv.Atoi(string(c)); err == nil {
					ret = append(ret, v)
				}
			}
		}
	}

	return ret
}

func acceptOTRRequest(p policies, msg []byte) (otrVersion, bool) {
	versions := parseOTRQueryMessage(msg)

	for _, v := range versions {
		switch {
		case v == 3 && p.has(allowV3):
			return otrV3{}, true
		case v == 2 && p.has(allowV2):
			return otrV2{}, true
		}
	}

	return nil, false
}

func (c *Conversation) sendDHCommit() (toSend []byte, err error) {
	c.ourInstanceTag = generateInstanceTag()

	toSend, err = c.dhCommitMessage()

	if err == nil {
		c.ensureAKE()
		c.ake.state = authStateAwaitingDHKey{}
		c.keys.ourKeyID = 0
		c.keys.ourCurrentDHKeys = dhKeyPair{}
	}

	return
}

func (c *Conversation) receiveQueryMessage(msg []byte) ([]byte, error) {
	v, ok := acceptOTRRequest(c.policies, msg)
	if !ok {
		return nil, errInvalidVersion
	}

	//TODO set the version for every existing otrContext
	c.version = v

	return c.sendDHCommit()
}

func (c Conversation) queryMessage() string {
	queryMessage := "?OTRv"
	if c.policies.has(allowV2) {
		queryMessage += "2"
	}
	if c.policies.has(allowV3) {
		queryMessage += "3"
	}
	return queryMessage + "?"
}
