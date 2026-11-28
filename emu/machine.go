package emu

import (
	"encoding/json"
	"math"
	"sort"
)

// EventData carries a device-event payload. Raw preserves the exact JSON
// bytes as injected, so input logs, replays and snapshots round-trip
// payloads verbatim; M is the lazily-parsed view used by device handlers.
type EventData struct {
	Raw json.RawMessage
	M   map[string]any
}

func NewEventDataRaw(raw json.RawMessage) EventData {
	d := EventData{Raw: append(json.RawMessage(nil), raw...), M: map[string]any{}}
	_ = json.Unmarshal(raw, &d.M)
	return d
}

func NewEventData(pairs ...any) EventData {
	return NewEventDataRaw(Ordered(pairs...))
}

func (e EventData) MarshalJSON() ([]byte, error) {
	if len(e.Raw) > 0 {
		return []byte(e.Raw), nil
	}
	if e.M != nil {
		return json.Marshal(e.M)
	}
	return []byte("{}"), nil
}

func (e *EventData) UnmarshalJSON(data []byte) error {
	e.Raw = append(json.RawMessage(nil), data...)
	e.M = map[string]any{}
	return json.Unmarshal(data, &e.M)
}

func (e EventData) Clone() EventData {
	return NewEventDataRaw(e.Raw)
}

func (e EventData) Map() map[string]any {
	if e.M == nil {
		return map[string]any{}
	}
	return e.M
}

// Event appends one {id, cycle, type, ...data} record to the ring buffer.
type Event struct {
	ID    int64           `json:"id"`
	Cycle int64           `json:"cycle"`
	Type  string          `json:"type"`
	Extra json.RawMessage `json:"-"`
}

func (e Event) MarshalJSON() ([]byte, error) {
	return json.Marshal(MergeJSON(
		Ordered("id", e.ID, "cycle", e.Cycle, "type", e.Type),
		e.Extra))
}

// ScheduledEvent is one pending input; it fires when cycles reach Cycle.
type ScheduledEvent struct {
	Cycle float64   `json:"cycle"`
	Type  string    `json:"type"`
	Data  EventData `json:"data"`
}

// InputLogEntry is one recorded external input, replayable via /api/replay.
type InputLogEntry struct {
	Cycle float64   `json:"cycle"`
	Type  string    `json:"type"`
	Data  EventData `json:"data"`
}

// Symbol is one entry of a companion .MAP symbol file, which places the
// address first and the logical address second.
type Symbol struct {
	Address  int    `json:"address"`
	Physical int    `json:"physical"`
	Name     string `json:"name"`
	Segment  string `json:"segment"`
	Module   string `json:"module"`
}

// Breakpoint {space, address, bbr?}.
type Breakpoint struct {
	Space   string `json:"space"`
	Address int    `json:"address"`
	Bbr     *int   `json:"bbr,omitempty"`
}

// Watchpoint {kind, space, address, length}.
type Watchpoint struct {
	Kind    string `json:"kind"`
	Space   string `json:"space"`
	Address int    `json:"address"`
	Length  int    `json:"length"`
}

// Patch entry; outputSha256 is appended after creation as in JS.
type Patch struct {
	ID           int    `json:"id"`
	Offset       int    `json:"offset"`
	Before       string `json:"before"`
	After        string `json:"after"`
	InputSha256  string `json:"inputSha256"`
	OutputSha256 string `json:"outputSha256"`
}

// TraceRow is one instruction-trace record served by /api/trace.
type TraceRow struct {
	PC       int    `json:"pc"`
	Physical int    `json:"physical"`
	Bbr      int    `json:"bbr"`
	Cycle    int64  `json:"cycle"`
	Bytes    string `json:"bytes"`
	A        int    `json:"a"`
	F        int    `json:"f"`
	SP       int    `json:"sp"`
}

// Fault {message, pc, physical}.
type Fault struct {
	Message  string `json:"message"`
	PC       int    `json:"pc"`
	Physical int    `json:"physical"`
}

// BytesArray marshals [][]byte as nested numeric arrays (JS shape).
type BytesArray []ByteList

// Devices is the full peripheral state. Field ORDER IS LOAD-BEARING: JSON
// objects serialize in field order, and snapshots plus the /api/state
// payload is part of the snapshot/state wire format. The
// devices_order tests enforce this — when adding a field, insert it where
// the modeled hardware state grouping suggests and update the tests.
//
// Numeric fields that hold simulated-cycle positions are float64 because the
// reference stored them as JS numbers; comparisons against CPU.Cycles (int64)
// convert explicitly.
type Devices struct {
	VFDState
	Eeprom            ByteList             `json:"eeprom"`
	EeBits            []int                `json:"eeBits"`
	EeOut             []int                `json:"eeOut"`
	EeRead            bool                 `json:"eeRead"`
	EeWriteEnabled    bool                 `json:"eeWriteEnabled"`
	EeData            []int                `json:"eeData"`
	EeAddress         int                  `json:"eeAddress"`
	CoinUART          *CoinUART            `json:"coinUART"`
	FlashCommands     []*FlashCommandState `json:"flashCommands"`
	OffHook           bool                 `json:"offHook"`
	LineConnected     bool                 `json:"lineConnected"`
	Reversed          bool                 `json:"reversed"`
	Ring              bool                 `json:"ring"`
	CoverOpen         bool                 `json:"coverOpen"`
	CashboxRemoved    bool                 `json:"cashboxRemoved"`
	IpQueue           BytesArray           `json:"ipQueue"`
	IpTx              ByteList             `json:"ipTx"`
	IpRxActive        ByteList             `json:"ipRxActive"`
	AsciRx            []ByteList           `json:"asciRx"`
	AsciNextRx        []float64            `json:"asciNextRx"`
	AsciNextTx        []float64            `json:"asciNextTx"`
	AsciTx            []ByteList           `json:"asciTx"`
	CoinTx            ByteList             `json:"coinTx"`
	CoinCommand       ByteList             `json:"coinCommand"`
	CoinAccept        bool                 `json:"coinAccept"`
	CoinEnabled       []bool               `json:"coinEnabled"`
	CoinEscrow        []int                `json:"coinEscrow"`
	CoinCollected     []int                `json:"coinCollected"`
	CoinReturned      []int                `json:"coinReturned"`
	Card              *int                 `json:"card"`
	Reader            *Reader              `json:"reader"`
	Modem             *Modem               `json:"modem"`
	Dma0Interrupt     bool                 `json:"dma0Interrupt"`
	Dma1Interrupt     bool                 `json:"dma1Interrupt"`
	Dialed            string               `json:"dialed"`
	CallState         int                  `json:"callState"`
	SerialPending     int                  `json:"serialPending"`
	SerialEdges       int                  `json:"serialEdges"`
	CsioInterrupt     bool                 `json:"csioInterrupt"`
	TimerAccum        float64              `json:"timerAccum"`
	TimerRead         bool                 `json:"timerRead"`
	ExternalAccum     float64              `json:"externalAccum"`
	ExternalPending   bool                 `json:"externalPending"`
	TelephonyResetDue *float64             `json:"telephonyResetDue"`
	Scheduled         []ScheduledEvent     `json:"scheduled"`
	Voice             ByteList             `json:"voice"`
	UnknownCommands   ByteList             `json:"unknownCommands"`
	Management        string               `json:"management"`
}

// Machine is the emulated phone: CPU, memory, ports and devices.
type Machine struct {
	Board          *Board
	IsMTR2         bool    // cached Board.Family == "mtr2" for hot paths
	externalPeriod float64 // cached MsCycles(10)
	mmuBA, mmuCA   int     // cached CBAR-derived region bases
	mmuBBR, mmuCBR int     // cached bank/common page offsets
	CPU            CPU
	Original       []byte
	ROM            []byte
	ROMGeneration  uint64 // bumped on every Load; identity for the NCC bridge
	Filename       string // "" means no firmware loaded (JS null)
	Symbols        []Symbol
	Breakpoints    []Breakpoint
	Watchpoints    []Watchpoint
	Patches        []Patch
	TraceEnabled   bool
	TraceLimit     int
	Memory         []byte
	Flash          []byte // nil on mtr1
	Ports          []byte
	Devices        *Devices
	Running        bool
	StopReason     string
	Fault          *Fault
	Events         []Event
	EventID        int64
	Trace          []TraceRow
	Hit            json.RawMessage // nil when none
	InputLog       []InputLogEntry
	OnSerialTx     func(ch int, v byte)
	// Test hooks: when non-nil they replace the port device logic,
	// allowing flat-memory CPU unit tests.
	InputOverride  func(port int) int
	OutputOverride func(port, value int)
	ReadOverride   func(a int) int
	WriteOverride  func(a, v int)
}

func NewMachine(boardID string) *Machine {
	b, err := SelectBoard(boardID, 0)
	if err != nil {
		b = Boards["mtr1"]
	}
	m := &Machine{Board: b, TraceLimit: 1000, Symbols: []Symbol{},
		Breakpoints: []Breakpoint{}, Watchpoints: []Watchpoint{}, Patches: []Patch{}}
	m.applyBoardCaches()
	m.Reset(false)
	return m
}

func (m *Machine) applyBoardCaches() {
	m.IsMTR2 = m.Board.Family == "mtr2"
	m.externalPeriod = m.MsCycles(10)
}

func (m *Machine) MsCycles(ms float64) float64 {
	return math.Round(ms * float64(m.Board.ClockHz) / 1000)
}

func (m *Machine) RelayState() int { return int(m.Ports[m.Board.Control]) & 7 }

// Reset reinitializes the CPU and all devices. Retention rules:
//   - warm reset: battery-backed RAM (memory from Board.RamStart up) and
//     EEPROM contents survive;
//   - cold reset: RAM reads back as zero-filled and EEPROM as all-ones,
//     as after a battery loss;
//   - the card reader survives BOTH reset kinds (a physically inserted card
//     does not fall out when the CPU resets) — only its transport state
//     (samples, counters, clocking) is re-initialized.
//
// MTR2 note: firmware lives in Flash, not in the memory array; the memory
// array holds only the RAM region, and Flash is (re)created from the loaded
// ROM when absent so warm resets keep firmware-programmed flash changes.
func (m *Machine) Reset(cold bool) {
	var ram []byte
	var eeprom ByteList
	if !cold && m.Memory != nil {
		ram = append([]byte(nil), m.Memory[m.Board.RamStart:]...)
	}
	if !cold && m.Devices != nil {
		eeprom = m.Devices.Eeprom
	}
	// JS keeps the reader across cold resets too: `const reader = this.devices?.reader`.
	var reader *Reader
	if m.Devices != nil {
		reader = m.Devices.Reader
	}
	m.Memory = make([]byte, 0x100000)
	for i := 0; i < m.Board.RamStart; i++ {
		m.Memory[i] = 255
	}
	if m.Board.Family == "mtr2" {
		if m.Flash == nil {
			m.Flash = make([]byte, 0x100000)
			for i := range m.Flash {
				m.Flash[i] = 255
			}
			copy(m.Flash, m.ROM)
		}
	} else {
		copy(m.Memory, m.ROM)
	}
	if ram != nil {
		copy(m.Memory[m.Board.RamStart:], ram)
	}
	m.Ports = make([]byte, 256)
	m.Ports[0x3a] = 0xf0
	m.Ports[4] = 2
	m.Ports[5] = 2
	m.Ports[0x34] = 1
	m.refreshMMUCache()

	d := &Devices{}
	d.VFDState = newVFDState()
	if eeprom == nil {
		eeprom = make(ByteList, m.Board.EeWords*2)
		for i := range eeprom {
			eeprom[i] = 255
		}
	}
	d.Eeprom = eeprom
	d.EeBits = []int{}
	d.EeOut = []int{}
	d.EeData = []int{}
	d.CoinUART = CoinUARTState()
	d.FlashCommands = newFlashCommands()
	d.LineConnected = true
	d.IpQueue = BytesArray{}
	d.IpTx = ByteList{}
	d.IpRxActive = ByteList{}
	d.AsciRx = []ByteList{{}, {}}
	d.AsciNextRx = []float64{0, 0}
	d.AsciNextTx = []float64{0, 0}
	d.AsciTx = []ByteList{{}, {}}
	d.CoinTx = ByteList{}
	d.CoinCommand = ByteList{}
	d.CoinAccept = true
	d.CoinEnabled = make([]bool, 16)
	for i := range d.CoinEnabled {
		d.CoinEnabled[i] = true
	}
	d.CoinEscrow = []int{}
	d.CoinCollected = []int{}
	d.CoinReturned = []int{}
	d.Reader = readerState(reader)
	d.Modem = modemState()
	d.Scheduled = []ScheduledEvent{}
	d.Voice = ByteList{}
	d.UnknownCommands = ByteList{}
	d.Management = "offline"
	m.Devices = d

	m.CPU.Reset()
	m.Running = false
	m.StopReason = "reset"
	m.Fault = nil
	m.Events = []Event{}
	m.EventID = 0
	m.Trace = []TraceRow{}
	m.Hit = nil
	m.InputLog = []InputLogEntry{}
	m.Event("reset", Ordered("cold", cold))
}

// Load installs a firmware image: the board profile is selected (explicit,
// or 'auto' by size — above 256 KiB means MTR2), the image is copied into
// fresh ROM/original buffers (bumping ROMGeneration so the NCC bridge ends
// any stale session), symbols replace any previous map, patches/breakpoints/
// watchpoints are cleared, and a cold reset follows. The filename is
// recorded verbatim for state and session files.
func (m *Machine) Load(data []byte, filename string, symbols []Symbol, board string) error {
	selected, err := SelectBoard(board, len(data))
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > selected.MaxROM {
		return errf("ROM must contain 1..%d bytes (%s hardware profile)", selected.MaxROM, selected.ID)
	}
	m.Board = selected
	m.applyBoardCaches()
	m.Flash = nil
	m.Original = append([]byte(nil), data...)
	m.ROM = append([]byte(nil), data...)
	m.ROMGeneration++
	m.Filename = filename
	m.Symbols = symbols
	m.Patches = []Patch{}
	m.Breakpoints = []Breakpoint{}
	m.Watchpoints = []Watchpoint{}
	m.Reset(true)
	m.Event("firmware.loaded", Ordered("filename", filename, "board", selected.ID,
		"size", len(data), "sha256", Hash(data)))
	return nil
}

// BootSectorEmpty reports whether an MTR2 flash image lacks a reset vector in
// the sector the CPU fetches from at power-on. At reset, port 0xC0 selects flash
// sector 0xC0000 (the bootstrap, which lives in the upper flash chip). A bare
// 512 KiB half — e.g. only the U2 application without the X1 bootstrap — leaves
// that sector erased, so the CPU fetches 0xFF and runs away into RST 0x38.
// Always false on MTR1, where the ROM is mapped directly.
func (m *Machine) BootSectorEmpty() bool {
	const resetSector = 0xC0000
	if !m.IsMTR2 || len(m.Flash) < resetSector+3 {
		return false
	}
	return m.Flash[resetSector] == 0xFF &&
		m.Flash[resetSector+1] == 0xFF &&
		m.Flash[resetSector+2] == 0xFF
}

// Physical applies Z180 MMU translation (CBAR/BBR/CBR) using cached decode.
func (m *Machine) Physical(address int) int {
	address &= 65535
	add := 0
	if address >= m.mmuCA {
		add = m.mmuCBR
	} else if address >= m.mmuBA {
		add = m.mmuBBR
	}
	return (address + add) & 0xfffff
}

// refreshMMUCache recomputes the cached MMU decode after 0x38/0x39/0x3a change.
func (m *Machine) refreshMMUCache() {
	cbar := int(m.Ports[0x3a])
	m.mmuBA = (cbar & 15) << 12
	m.mmuCA = (cbar >> 4) << 12
	m.mmuCBR = int(m.Ports[0x38]) << 12
	m.mmuBBR = int(m.Ports[0x39]) << 12
}

func (m *Machine) ReadPhysical(p int) int {
	if m.IsMTR2 {
		if offset := m.FlashOffset(p); offset >= 0 {
			return m.FlashRead(offset)
		}
	}
	return int(m.Memory[p])
}

func (m *Machine) WritePhysical(p, v int) {
	if m.IsMTR2 {
		if offset := m.FlashOffset(p); offset >= 0 {
			m.FlashWrite(offset, v)
			return
		}
	} else if p < 0x40000 {
		return
	}
	m.Memory[p] = byte(v)
}

func (m *Machine) Read(a int) int {
	if m.ReadOverride != nil {
		return m.ReadOverride(a & 65535)
	}
	// Inlined Physical+ReadPhysical: this is the hottest path (every fetch).
	address := a & 65535
	add := 0
	if address >= m.mmuCA {
		add = m.mmuCBR
	} else if address >= m.mmuBA {
		add = m.mmuBBR
	}
	p := (address + add) & 0xfffff
	var v int
	if m.IsMTR2 {
		if offset := m.FlashOffset(p); offset >= 0 {
			v = m.FlashRead(offset)
		} else {
			v = int(m.Memory[p])
		}
	} else {
		v = int(m.Memory[p])
	}
	if len(m.Watchpoints) > 0 {
		m.watch("read", address, p, v)
	}
	return v
}

func (m *Machine) Write(a, v int) {
	if m.WriteOverride != nil {
		m.WriteOverride(a&65535, v&255)
		return
	}
	address := a & 65535
	add := 0
	if address >= m.mmuCA {
		add = m.mmuCBR
	} else if address >= m.mmuBA {
		add = m.mmuBBR
	}
	p := (address + add) & 0xfffff
	if m.IsMTR2 {
		if offset := m.FlashOffset(p); offset >= 0 {
			m.FlashWrite(offset, v)
		} else {
			m.Memory[p] = byte(v)
		}
	} else if p >= 0x40000 {
		m.Memory[p] = byte(v)
	}
	if len(m.Watchpoints) > 0 {
		m.watch("write", address, p, v)
	}
}

func (m *Machine) watch(kind string, logical, physical, value int) {
	for _, w := range m.Watchpoints {
		a := logical
		if w.Space == "physical" {
			a = physical
		}
		if w.Kind == kind && a >= w.Address && a < w.Address+w.Length {
			m.Hit = Ordered("type", "watchpoint", "kind", w.Kind, "space", w.Space,
				"address", w.Address, "length", w.Length,
				"logical", logical, "physical", physical, "value", value)
			break
		}
	}
}

// Event appends {id, cycle, type, ...data} to the ring buffer. The buffer
// holds the 2000 most recent events; on overflow the oldest 500 are dropped
// in one go (so ids remain contiguous within a burst). Reset restarts ids at
// zero; clients detect that as an event-stream reset.
func (m *Machine) Event(typ string, data json.RawMessage) Event {
	m.EventID++
	e := Event{ID: m.EventID, Cycle: m.CPU.Cycles, Type: typ, Extra: data}
	m.Events = append(m.Events, e)
	if len(m.Events) > 2000 {
		m.Events = append([]Event(nil), m.Events[500:]...)
	}
	return e
}

func (m *Machine) QueueIP(data []byte) error {
	if len(m.Devices.IpQueue) >= 256 {
		return errPlain("Telephony queue full")
	}
	m.Devices.IpQueue = append(m.Devices.IpQueue, ByteList(append([]byte(nil), data...)))
	m.Event("telephony.rx.queued", Ordered("bytes", ByteList(data)))
	return nil
}

func (m *Machine) Schedule(after float64, typ string, data EventData) {
	m.Devices.Scheduled = append(m.Devices.Scheduled,
		ScheduledEvent{Cycle: float64(m.CPU.Cycles) + after, Type: typ, Data: data})
}

// DrainEvents dispatches scheduled inputs whose cycle has arrived, in stable
// cycle order. Events scheduled by a dispatched event join the same queue
// and may fire within the same pass, so the slice is re-read after every
// dispatch.
func (m *Machine) DrainEvents() error {
	if len(m.Devices.Scheduled) == 0 {
		return nil
	}
	sort.SliceStable(m.Devices.Scheduled, func(i, j int) bool {
		return m.Devices.Scheduled[i].Cycle < m.Devices.Scheduled[j].Cycle
	})
	for len(m.Devices.Scheduled) > 0 && m.Devices.Scheduled[0].Cycle <= float64(m.CPU.Cycles) {
		e := m.Devices.Scheduled[0]
		m.Devices.Scheduled = m.Devices.Scheduled[1:]
		if err := m.DeviceEvent(e.Type, e.Data, false); err != nil {
			return err
		}
	}
	return nil
}
