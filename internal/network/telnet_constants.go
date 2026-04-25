package network

// Telnet protocol constants as defined in RFC 854 and RFC 855.
const (
	// IAC (Interpret As Command) marks the start of a Telnet command sequence.
	IAC byte = 255

	// Negotiation commands
	DONT byte = 254 // Sender refuses to perform option
	DO   byte = 253 // Sender requests receiver to perform option
	WONT byte = 252 // Sender refuses to perform option
	WILL byte = 251 // Sender will perform option

	// Subnegotiation
	SB byte = 250 // Start of subnegotiation
	SE byte = 240 // End of subnegotiation

	// Telnet options
	ECHO    byte = 1   // Echo option (RFC 857)
	SGA     byte = 3   // Suppress Go Ahead (RFC 858)
	TTYPE   byte = 24  // Terminal Type (RFC 1091)
	EOR     byte = 25  // End of Record (RFC 885)
	NAWS    byte = 31  // Negotiate About Window Size (RFC 1073)
	CHARSET byte = 42  // Character Set (RFC 2066)
	MSDP    byte = 69  // Mud Server Data Protocol
	MSSP    byte = 70  // Mud Server Status Protocol
	MCCP2   byte = 86  // Mud Client Compression Protocol (v2)
	MXP     byte = 91  // Mud eXtension Protocol
	GMCP    byte = 201 // Generic Mud Communication Protocol

	// Other commands
	GA  byte = 249 // Go Ahead
	NOP byte = 241 // No Operation

	// TTYPE subnegotiation commands
	TTYPE_IS   byte = 0
	TTYPE_SEND byte = 1
)
