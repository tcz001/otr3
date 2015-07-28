package otr3

import "errors"

type smpStateBase struct{}
type smpStateExpect1 struct{ smpStateBase }
type smpStateExpect2 struct{ smpStateBase }
type smpStateExpect3 struct{ smpStateBase }
type smpStateExpect4 struct{ smpStateBase }

var errUnexpectedMessage = errors.New("unexpected SMP message")

type smpMessage interface {
	receivedMessage(*conversation) (smpMessage, error)
	tlv() tlv
}

type smpState interface {
	receiveMessage1(*conversation, smp1Message) (smpState, smpMessage, error)
	receiveMessage2(*conversation, smp2Message) (smpState, smpMessage, error)
	receiveMessage3(*conversation, smp3Message) (smpState, smpMessage, error)
	receiveMessage4(*conversation, smp4Message) (smpState, smpMessage, error)
}

func (c *conversation) restart() []byte {
	var ret smpMessage
	c.smp.state, ret, _ = abortStateMachine()
	return ret.tlv().serialize()
}

func abortStateMachine() (smpState, smpMessage, error) {
	return abortStateMachineWith(nil)
}

func abortStateMachineWith(e error) (smpState, smpMessage, error) {
	return smpStateExpect1{}, smpMessageAbort{}, e
}

func (c *conversation) receiveSMP(m smpMessage) ([]byte, error) {
	toSend, err := m.receivedMessage(c)

	if err != nil {
		return nil, err
	}

	if toSend == nil {
		return nil, nil
	}

	return toSend.tlv().serialize(), nil
}

func (smpStateBase) receiveMessage1(c *conversation, m smp1Message) (smpState, smpMessage, error) {
	return abortStateMachine()
}

func (smpStateBase) receiveMessage2(c *conversation, m smp2Message) (smpState, smpMessage, error) {
	return abortStateMachine()
}

func (smpStateBase) receiveMessage3(c *conversation, m smp3Message) (smpState, smpMessage, error) {
	return abortStateMachine()
}

func (smpStateBase) receiveMessage4(c *conversation, m smp4Message) (smpState, smpMessage, error) {
	return abortStateMachine()
}

func (smpStateExpect1) receiveMessage1(c *conversation, m smp1Message) (smpState, smpMessage, error) {
	err := c.verifySMP1(m)
	if err != nil {
		return abortStateMachineWith(err)
	}

	ret, ok := c.generateSMP2(c.smp.secret, m)
	if !ok {
		return abortStateMachineWith(errShortRandomRead)
	}

	return smpStateExpect3{}, ret.msg, nil
}

func (smpStateExpect2) receiveMessage2(c *conversation, m smp2Message) (smpState, smpMessage, error) {
	//TODO: make sure c.s1 is stored when it is generated

	err := c.verifySMP2(c.smp.s1, m)
	if err != nil {
		return abortStateMachineWith(err)
	}

	ret, ok := c.generateSMP3(c.smp.secret, c.smp.s1, m)
	if !ok {
		return abortStateMachineWith(errShortRandomRead)
	}

	return smpStateExpect4{}, ret.msg, nil
}

func (smpStateExpect3) receiveMessage3(c *conversation, m smp3Message) (smpState, smpMessage, error) {
	//TODO: make sure c.s2 is stored when it is generated

	err := c.verifySMP3(c.smp.s2, m)
	if err != nil {
		return abortStateMachineWith(err)
	}

	err = c.verifySMP3ProtocolSuccess(c.smp.s2, m)
	if err != nil {
		return abortStateMachineWith(err)
	}

	ret, ok := c.generateSMP4(c.smp.secret, c.smp.s2, m)
	if !ok {
		return abortStateMachineWith(errShortRandomRead)
	}

	return smpStateExpect1{}, ret.msg, nil
}

func (smpStateExpect4) receiveMessage4(c *conversation, m smp4Message) (smpState, smpMessage, error) {
	//TODO: make sure c.s3 is stored when it is generated

	err := c.verifySMP4(c.smp.s3, m)
	if err != nil {
		return abortStateMachineWith(err)
	}

	err = c.verifySMP4ProtocolSuccess(c.smp.s1, c.smp.s3, m)
	if err != nil {
		return abortStateMachineWith(err)
	}

	return smpStateExpect1{}, nil, nil
}

func (m smp1Message) receivedMessage(c *conversation) (ret smpMessage, err error) {
	c.smp.state, ret, err = c.smp.state.receiveMessage1(c, m)
	return
}

func (m smp2Message) receivedMessage(c *conversation) (ret smpMessage, err error) {
	c.smp.state, ret, err = c.smp.state.receiveMessage2(c, m)
	return
}

func (m smp3Message) receivedMessage(c *conversation) (ret smpMessage, err error) {
	c.smp.state, ret, err = c.smp.state.receiveMessage3(c, m)
	return
}

func (m smp4Message) receivedMessage(c *conversation) (ret smpMessage, err error) {
	c.smp.state, ret, err = c.smp.state.receiveMessage4(c, m)
	return
}

func (m smpMessageAbort) receivedMessage(c *conversation) (ret smpMessage, err error) {
	c.smp.state = smpStateExpect1{}
	return
}

func (smpStateExpect1) String() string { return "SMPSTATE_EXPECT1" }
func (smpStateExpect2) String() string { return "SMPSTATE_EXPECT2" }
func (smpStateExpect3) String() string { return "SMPSTATE_EXPECT3" }
