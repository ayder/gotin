// Package command defines the typed command interface and all concrete command
// structs used for communication between the input parser and the application
// layer. Each command is a small, immutable value object.
package command

// Command is a sum-type marker interface. Only the concrete types defined in
// this package should implement it.
type Command interface{ isCommand() }

// ─── Connection ────────────────────────────────────────────────────────────

type Connect struct {
	Host string
	Port int
}
type Quit struct{}
type ConfirmConnect struct{}
type CancelConnect struct{}

func (*Connect) isCommand()        {}
func (*Quit) isCommand()           {}
func (*ConfirmConnect) isCommand() {}
func (*CancelConnect) isCommand()  {}

// ─── Aliases ───────────────────────────────────────────────────────────────

type AliasAdd struct{ Pattern, Expansion string }
type AliasRemove struct{ Pattern string }
type AliasList struct{}
type AliasListConnections struct{}
type ConnectionAliasAdd struct {
	Name, Host string
	Port       int
	Auto       bool
}
type ConnectionAliasRemove struct{ Name string }

func (*AliasAdd) isCommand()              {}
func (*AliasRemove) isCommand()           {}
func (*AliasList) isCommand()             {}
func (*AliasListConnections) isCommand()  {}
func (*ConnectionAliasAdd) isCommand()    {}
func (*ConnectionAliasRemove) isCommand() {}

// ─── Triggers ──────────────────────────────────────────────────────────────

type TriggerAdd struct{ Pattern, Response string }
type TriggerRemove struct{ Pattern string }
type TriggerList struct{}

func (*TriggerAdd) isCommand()    {}
func (*TriggerRemove) isCommand() {}
func (*TriggerList) isCommand()   {}

// ─── Protocols ─────────────────────────────────────────────────────────────

type ProtoOn struct{ Name string }
type ProtoOff struct{ Name string }
type ProtoList struct{}

func (*ProtoOn) isCommand()   {}
func (*ProtoOff) isCommand()  {}
func (*ProtoList) isCommand() {}

// ─── Persistence ───────────────────────────────────────────────────────────

type Save struct{ Filename string }
type Load struct{ Filename string }

func (*Save) isCommand() {}
func (*Load) isCommand() {}

// ─── Mapper ────────────────────────────────────────────────────────────────

type MapCreate struct{ Filename string }
type MapPaths struct{ Directions string }
type MapDig struct{ Direction, Action string }
type MapUndo struct{}
type MapDelete struct{ Query string }
type MapGoto struct{ Query string }
type MapLink struct{ Direction, Target string }
type MapStart struct{ Query string }
type MapStop struct{}
type MapName struct{ Name string }
type MapSearch struct{ Query string }
type MapMermaid struct{ Scope string }
type MapShow struct{}
type MapRefresh struct{}
type MapInfo struct{}
type MapExit struct{}
type MapOption struct {
	Print    bool
	Strategy string
	Enable   bool
}

func (*MapCreate) isCommand()  {}
func (*MapPaths) isCommand()   {}
func (*MapDig) isCommand()     {}
func (*MapUndo) isCommand()    {}
func (*MapDelete) isCommand()  {}
func (*MapGoto) isCommand()    {}
func (*MapLink) isCommand()    {}
func (*MapStart) isCommand()   {}
func (*MapStop) isCommand()    {}
func (*MapName) isCommand()    {}
func (*MapSearch) isCommand()  {}
func (*MapMermaid) isCommand() {}
func (*MapShow) isCommand()    {}
func (*MapRefresh) isCommand() {}
func (*MapInfo) isCommand()    {}
func (*MapExit) isCommand()    {}
func (*MapOption) isCommand()  {}

// ─── UI ────────────────────────────────────────────────────────────────────

type ShowHelp struct{}

func (*ShowHelp) isCommand() {}
