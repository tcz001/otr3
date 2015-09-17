package otr3

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/gob"
	"io"
	"math/big"
)

type ake struct {
	secretExponent   *big.Int
	ourPublicValue   *big.Int
	theirPublicValue *big.Int

	r [16]byte

	encryptedGx []byte
	hashedGx    [sha256.Size]byte

	revealKey akeKeys
	sigKey    akeKeys

	state authState
	keys  keyManagementContext
}

func (c *Conversation) ensureAKE() {
	if c.ake != nil {
		return
	}

	c.initAKE()
}

func (c *Conversation) initAKE() {
	c.ake = &ake{
		state: authStateNone{},
	}
}

func (c *Conversation) calcAKEKeys(s *big.Int) {
	c.ssid, c.ake.revealKey, c.ake.sigKey = calculateAKEKeys(s)
}

func (c *Conversation) setSecretExponent(val *big.Int) {
	c.ake.secretExponent = new(big.Int).Set(val)
	c.ake.ourPublicValue = modExp(g1, val)
}

func (c *Conversation) calcDHSharedSecret() *big.Int {
	return modExp(c.ake.theirPublicValue, c.ake.secretExponent)
}

func (c *Conversation) generateEncryptedSignature(key *akeKeys) ([]byte, error) {
	verifyData := appendAll(c.ake.ourPublicValue, c.ake.theirPublicValue, &c.ourKey.PublicKey, c.ake.keys.ourKeyID)

	mb := sumHMAC(key.m1[:], verifyData)
	xb, err := c.calcXb(key, mb)

	if err != nil {
		return nil, err
	}

	return appendData(nil, xb), nil
}
func appendAll(one, two *big.Int, publicKey *PublicKey, keyID uint32) []byte {
	return appendWord(append(appendMPI(appendMPI(nil, one), two), publicKey.serialize()...), keyID)
}

func (c *Conversation) calcXb(key *akeKeys, mb []byte) ([]byte, error) {
	xb := c.ourKey.PublicKey.serialize()
	xb = appendWord(xb, c.ake.keys.ourKeyID)

	sigb, err := c.ourKey.Sign(c.rand(), mb)
	if err == io.ErrUnexpectedEOF {
		return nil, errShortRandomRead
	}

	if err != nil {
		return nil, err
	}

	// this error can't happen, since key.c is fixed to the correct size
	xb, _ = encrypt(key.c[:], append(xb, sigb...))

	return xb, nil
}

// dhCommitMessage = bob = x
// Bob ---- DH Commit -----------> Alice
func (c *Conversation) dhCommitMessage() ([]byte, error) {
	c.initAKE()
	c.ake.keys.ourKeyID = 0

	x, err := c.randMPI(make([]byte, 40))
	if err != nil {
		return nil, err
	}

	c.setSecretExponent(x)
	wipeBigInt(x)

	if err := c.randomInto(c.ake.r[:]); err != nil {
		return nil, err
	}

	// this can't return an error, since ake.r is of a fixed size that is always correct
	c.ake.encryptedGx, _ = encrypt(c.ake.r[:], appendMPI(nil, c.ake.ourPublicValue))
	return c.serializeDHCommit(c.ake.ourPublicValue), nil
}

func (c *Conversation) serializeDHCommit(public *big.Int) []byte {
	dhCommitMsg := dhCommit{
		encryptedGx: c.ake.encryptedGx,
		hashedGx:    sha256.Sum256(appendMPI(nil, public)),
	}
	return dhCommitMsg.serialize()
}

// dhKeyMessage = alice = y
// Alice -- DH Key --------------> Bob
func (c *Conversation) dhKeyMessage() ([]byte, error) {
	c.initAKE()

	y, err := c.randMPI(make([]byte, 40)[:])

	if err != nil {
		return nil, err
	}

	c.setSecretExponent(y)
	wipeBigInt(y)

	return c.serializeDHKey(), nil
}

func (c *Conversation) serializeDHKey() []byte {
	dhKeyMsg := dhKey{
		gy: c.ake.ourPublicValue,
	}

	return dhKeyMsg.serialize()
}

// revealSigMessage = bob = x
// Bob ---- Reveal Signature ----> Alice
func (c *Conversation) revealSigMessage() ([]byte, error) {
	c.calcAKEKeys(c.calcDHSharedSecret())
	c.ake.keys.ourKeyID++

	encryptedSig, err := c.generateEncryptedSignature(&c.ake.revealKey)
	if err != nil {
		return nil, err
	}

	macSig := sumHMAC(c.ake.revealKey.m2[:], encryptedSig)
	revealSigMsg := revealSig{
		r:            c.ake.r,
		encryptedSig: encryptedSig,
		macSig:       macSig,
	}

	return revealSigMsg.serialize(), nil
}

// sigMessage = alice = y
// Alice -- Signature -----------> Bob
func (c *Conversation) sigMessage() ([]byte, error) {
	c.ake.keys.ourKeyID++

	encryptedSig, err := c.generateEncryptedSignature(&c.ake.sigKey)
	if err != nil {
		return nil, err
	}

	macSig := sumHMAC(c.ake.sigKey.m2[:], encryptedSig)
	sigMsg := sig{
		encryptedSig: encryptedSig,
		macSig:       macSig,
	}

	return sigMsg.serialize(), nil
}

// processDHCommit = alice = y
// Bob ---- DH Commit -----------> Alice
func (c *Conversation) processDHCommit(msg []byte) error {
	dhCommitMsg := dhCommit{}
	err := dhCommitMsg.deserialize(msg)
	if err != nil {
		return err
	}

	c.ake.encryptedGx = dhCommitMsg.encryptedGx
	c.ake.hashedGx = dhCommitMsg.hashedGx

	return err
}

// processDHKey = bob = x
// Alice -- DH Key --------------> Bob
func (c *Conversation) processDHKey(msg []byte) (isSame bool, err error) {
	dhKeyMsg := dhKey{}
	err = dhKeyMsg.deserialize(msg)
	if err != nil {
		return false, err
	}

	if !isGroupElement(dhKeyMsg.gy) {
		return false, newOtrError("DH value out of range")
	}

	//If receive same public key twice, just retransmit the previous Reveal Signature
	if c.ake.theirPublicValue != nil {
		isSame = eq(c.ake.theirPublicValue, dhKeyMsg.gy)
		return
	}

	c.ake.theirPublicValue = dhKeyMsg.gy
	return
}

// processRevealSig = alice = y
// Bob ---- Reveal Signature ----> Alice
func (c *Conversation) processRevealSig(msg []byte) (err error) {
	revealSigMsg := revealSig{}
	err = revealSigMsg.deserialize(msg)
	if err != nil {
		return
	}

	r := revealSigMsg.r[:]
	theirMAC := revealSigMsg.macSig
	encryptedSig := revealSigMsg.encryptedSig

	decryptedGx := make([]byte, len(c.ake.encryptedGx))
	if err = decrypt(r, decryptedGx, c.ake.encryptedGx); err != nil {
		return
	}

	if err = checkDecryptedGx(decryptedGx, c.ake.hashedGx[:]); err != nil {
		return
	}

	if c.ake.theirPublicValue, err = extractGx(decryptedGx); err != nil {
		return
	}

	c.calcAKEKeys(c.calcDHSharedSecret())
	if err = c.processEncryptedSig(encryptedSig, theirMAC, &c.ake.revealKey); err != nil {
		return newOtrError("in reveal signature message: " + err.Error())
	}

	return nil
}

// processSig = bob = x
// Alice -- Signature -----------> Bob
func (c *Conversation) processSig(msg []byte) (err error) {
	sigMsg := sig{}
	err = sigMsg.deserialize(msg)
	if err != nil {
		return
	}

	theirMAC := sigMsg.macSig
	encryptedSig := sigMsg.encryptedSig

	if err := c.processEncryptedSig(encryptedSig, theirMAC, &c.ake.sigKey); err != nil {
		return newOtrError("in signature message: " + err.Error())
	}

	return nil
}

func (c *Conversation) checkedSignatureVerification(mb, sig []byte) error {
	rest, ok := c.theirKey.Verify(mb, sig)
	if !ok {
		return newOtrError("bad signature in encrypted signature")
	}

	if len(rest) > 0 {
		return errCorruptEncryptedSignature
	}

	return nil
}

func verifyEncryptedSignatureMAC(encryptedSig []byte, theirMAC []byte, keys *akeKeys) error {
	tomac := appendData(nil, encryptedSig)
	myMAC := sumHMAC(keys.m2[:], tomac)[:20]

	if len(myMAC) != len(theirMAC) || subtle.ConstantTimeCompare(myMAC, theirMAC) == 0 {
		return newOtrError("bad signature MAC in encrypted signature")
	}

	return nil
}

func (c *Conversation) parseTheirKey(key []byte) (sig []byte, keyID uint32, err error) {
	c.theirKey = &PublicKey{}
	rest, ok1 := c.theirKey.Parse(key)
	sig, keyID, ok2 := extractWord(rest)

	if !ok1 || !ok2 {
		return nil, 0, errCorruptEncryptedSignature
	}

	return
}

func (c *Conversation) expectedMessageHMAC(keyID uint32, keys *akeKeys) []byte {
	verifyData := appendAll(c.ake.theirPublicValue, c.ake.ourPublicValue, c.theirKey, keyID)
	return sumHMAC(keys.m1[:], verifyData)
}

func (c *Conversation) processEncryptedSig(encryptedSig []byte, theirMAC []byte, keys *akeKeys) error {
	if err := verifyEncryptedSignatureMAC(encryptedSig, theirMAC, keys); err != nil {
		return err
	}

	decryptedSig := encryptedSig
	if err := decrypt(keys.c[:], decryptedSig, encryptedSig); err != nil {
		return err
	}

	sig, keyID, err := c.parseTheirKey(decryptedSig)
	if err != nil {
		return err
	}

	mb := c.expectedMessageHMAC(keyID, keys)
	if err := c.checkedSignatureVerification(mb, sig); err != nil {
		return err
	}

	c.ake.keys.theirKeyID = keyID

	return nil
}

func extractGx(decryptedGx []byte) (*big.Int, error) {
	newData, gx, ok := extractMPI(decryptedGx)
	if !ok || len(newData) > 0 {
		return gx, newOtrError("gx corrupt after decryption")
	}

	if !isGroupElement(gx) {
		return gx, newOtrError("DH value out of range")
	}

	return gx, nil
}

func sumHMAC(key, data []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}

func checkDecryptedGx(decryptedGx, hashedGx []byte) error {
	digest := sha256.Sum256(decryptedGx)

	if subtle.ConstantTimeCompare(digest[:], hashedGx[:]) == 0 {
		return newOtrError("bad commit MAC in reveal signature message")
	}

	return nil
}

func (c ake) GobEncode() ([]byte, error) {
	w := new(bytes.Buffer)
	encoder := gob.NewEncoder(w)
	if c.secretExponent != nil {
		err := encoder.Encode(c.secretExponent)
		if err != nil {
			return nil, err
		}
	}
	if c.ourPublicValue != nil {
		err := encoder.Encode(c.ourPublicValue)
		if err != nil {
			return nil, err
		}
	}
	if c.theirPublicValue != nil {
		err := encoder.Encode(c.theirPublicValue)
		if err != nil {
			return nil, err
		}
	}
	err := encoder.Encode(c.r)
	if err != nil {
		return nil, err
	}
	err = encoder.Encode(c.encryptedGx)
	if err != nil {
		return nil, err
	}
	err = encoder.Encode(c.hashedGx)
	if err != nil {
		return nil, err
	}
	err = encoder.Encode(c.revealKey)
	if err != nil {
		return nil, err
	}
	err = encoder.Encode(c.sigKey)
	if err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}

func (c *ake) GobDecode(buf []byte) error {
	r := bytes.NewBuffer(buf)
	decoder := gob.NewDecoder(r)
	err := decoder.Decode(&c.secretExponent)
	if err != nil {
		return err
	}
	err = decoder.Decode(&c.ourPublicValue)
	if err != nil {
		return err
	}
	err = decoder.Decode(&c.theirPublicValue)
	if err != nil {
		return err
	}
	err = decoder.Decode(&c.encryptedGx)
	if err != nil {
		return err
	}
	err = decoder.Decode(&c.hashedGx)
	if err != nil {
		return err
	}
	err = decoder.Decode(&c.revealKey)
	if err != nil {
		return err
	}
	err = decoder.Decode(&c.sigKey)
	if err != nil {
		return err
	}
	return nil
}
