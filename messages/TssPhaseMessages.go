package messages

type Algo int

const (
	EDDSAKEYGEN1       = "EDDSAKGRound1Message"
	EDDSAKEYGEN2a      = "EDDSAKGRound2Message1"
	EDDSAKEYGEN2b      = "EDDSAKGRound2Message2"
	EDDSAKEYSIGN1      = "EDDSASignRound1Message"
	EDDSAKEYSIGN2      = "EDDSASignRound2Message"
	EDDSAKEYSIGN3      = "EDDSASignRound3Message"
	EDDSAKEYREGROUP1   = "EDDSADGRound1Message"
	EDDSAKEYREGROUP2   = "EDDSADGRound2Message"
	EDDSAKEYREGROUP3a  = "EDDSADGRound3Message1"
	EDDSAKEYREGROUP3b  = "EDDSADGRound3Message2"
	EDDSAKEYREGROUP4   = "EDDSADGRound4Message"
	EDDSAKEYGENROUNDS  = 3
	EDDSAKEYSIGNROUNDS = 3

	KEYGEN1          = "KGRound1Message"
	KEYGEN2aUnicast  = "KGRound2Message1"
	KEYGEN2b         = "KGRound2Message2"
	KEYGEN3          = "KGRound3Message"
	KEYSIGN1aUnicast = "SignRound1Message1"
	KEYSIGN1b        = "SignRound1Message2"
	KEYSIGN2Unicast  = "SignRound2Message"
	KEYSIGN3         = "SignRound3Message"
	KEYSIGN4         = "SignRound4Message"
	KEYSIGN5         = "SignRound5Message"
	KEYSIGN6         = "SignRound6Message"
	KEYSIGN7         = "SignRound7Message"
	KEYSIGN8         = "SignRound8Message"
	KEYSIGN9         = "SignRound9Message"
	TSSKEYGENROUNDS  = 4
	TSSKEYSIGNROUNDS = 9

	ECDSAKEYGEN Algo = iota
	ECDSAKEYSIGN
	EDDSAKEYGEN
	EDDSAKEYSIGN
)
