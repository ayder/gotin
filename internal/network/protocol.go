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
	ECHO byte = 1 // Echo option (RFC 857)
)
