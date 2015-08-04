package otr3

import "time"

// How long after sending a packet should we wait to send a heartbeat?
const heartbeatInterval = 60 * time.Second

type heartbeatContext struct {
	lastSent time.Time
}

func (c *Conversation) maybeHeartbeat(plain, toSend []byte, err error) ([]byte, []byte, []byte, error) {
	if err != nil {
		return nil, nil, nil, err
	}
	tsExtra, e := c.potentialHeartbeat(plain)
	return plain, toSend, tsExtra, e
}

// We need to update lastsent all the time. Sigh

func (c *Conversation) potentialHeartbeat(plain []byte) (toSend []byte, err error) {
	if plain != nil {
		now := time.Now()
		if c.heartbeat.lastSent.Before(now.Add(-heartbeatInterval)) {
			dataMsg, err := c.genDataMsgWithFlag([]byte{}, messageFlagIgnoreUnreadable)
			if err != nil {
				return nil, err
			}
			toSend = dataMsg.serialize()
			c.heartbeat.lastSent = now
			messageEventHeartbeatSent(c)
		}
	}
	return
}
