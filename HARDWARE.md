# Hardware reference (Millennium MTR firmware)

Notes from an independent static reverse-engineering of the Nortel/Quortech Millennium **MTR**
firmware (Zilog Z180), offered to complement this emulator. Full write-up and tooling:
<https://github.com/tasmaniandroid/millennium-mtr-re>. Everything below is from static analysis of
the ROMs in the `muccc/millennium` collection, cross-checked against this emulator's device models
(they agree — this documents the firmware side and adds the acceptance details the emulator leaves
to the firmware). Confidence is noted where it matters.

## CPU
Zilog **Z180** (HD64180-compatible). Confirmed by opcode census plus Z180-only opcodes present in
the images (`MLT`, `TST m`, `IN0`/`OUT0`/`TSTIO`). Clocks match this emulator's profiles: MTR1
6.144 MHz, MTR2 12.288 MHz.

## Memory map (from the firmware's own MMU setup)
The reset path programs the Z180 MMU (`CBAR`/`CBR`/`BBR` at I/O 0x3A/0x38/0x39). **`CBAR=0x85` is
universal across every MTR image**, giving one logical layout:

| Logical | Region | Translation |
|---|---|---|
| `0x0000–0x4FFF` | resident ROM (20 KB) | 1:1 |
| `0x5000–0x7FFF` | **bank window** (12 KB) | `phys = logical + (BBR<<12)` |
| `0x8000–0xFFFF` | external SRAM (32 KB) | `phys = logical + (CBR<<12)` |

**`CBR` is the board-generation discriminator** (handy for board auto-detection straight from the
bytes, not just image size):

| | Flash | `CBR` | SRAM physical |
|---|---|---|---|
| **MTR1** | 256 KB (27C2001) | `0x38` | `0x40000–0x47FFF` |
| **MTR2** | 512 KB-per-chip (29F040B) | `0xB8` | `0xC0000–0xC7FFF` |

(One outlier: `12AAV08` uses `CBAR=0x96` → window `0x6000–0x8FFF`.) Detection heuristic: scan for
`3E xx ED 39 3A` (CBAR) / `3E xx ED 39 38` (CBR).

## MTR2 flash is two chips (U2 + X1)
The MTR2 "2.0 SMD" board carries **two 512 KB flash chips** = a 1 MB flash. This matches the
emulator's dual-chip `FlashOffset` model; for reference, the image layout we see:

| Flash sector | Chip / offset | Contents |
|---|---|---|
| sector 0 (`0x00000`) | **U2** | main application |
| sector C (`0xC0000`) | **X1** @`0x40000` | **boot loader** — the CPU resets here (`DI; LD C,1; JP …`) |
| sectors D/E (`0xD0000`/`0xE0000`) | X1 | **download-agent (DLA)** images |
| sector F (`0xF0000`) | — | unused (erased) |

So a complete MTR2 flash is `U2 + X1` concatenated; a bare U2 alone has an empty reset sector and
will not boot. (X1 is mostly `0xFF` but is **not** erased filler — it is the bootstrap + DLA.)

## Banked far-call ABI (for disassembly / symbol recovery)
Paged code is reached through a resident **bank-entry table**: each banked target has a 6-byte stub
`CALL <dispatcher> ; DB addr_lo, addr_hi, bank`, and ordinary code reaches a banked routine with a
plain `CALL <stub>`. The dispatcher reads the inline `(addr, bank)` and switches `BBR`, so
`phys = addr + (bank<<12)` (`addr` in `0x5000–0x7FFF`). The dispatcher address is per-build (e.g.
0x3260 on NKA1X02) and auto-finds as the dominant CALL target whose descriptor lands in the window.

This is **Softools' `MMU180` bank-switch runtime** — the firmware was built with the Softools
**SC180 / SASM180 / SLINK** Z180 toolchain (© Softools, Inc. / Bill Auerbach), identified from a
byte-exact match of the shipped `MMU180.ASM`. Canonical symbols, if useful in a disassembler:
`__bank_call` (dispatcher, 0x3260 on NKA1X02), `__bank_jump` (0x32AD), `__bank_ret` (0x32C9),
`__bank_init` (0x324E), and the `__bank_entry` stub table (0x32F2). A standalone Ghidra analyzer that
rebuilds this for any image is in the RE repo.

## Peripheral I/O port map (observed; corrected)
| Port(s) | Device | Notes |
|---|---|---|
| `0x00–0x09` | Z180 ASCI0 / ASCI1 — two UARTs | modem/datapump + TTY/craft |
| `0x0A/0x0B` | Z180 CSI/O — **ISO7816 microprocessor smart card** | the admin "Smart Card Test" path |
| `0x40–0x42` | **Microwire serial config EEPROM** (93C46 MTR1 / 93C66 MTR2) | CS=`0x40` bit6, CLK=`0x42` bit7, DO=`0x40` bit3 — matches this emulator's `EepromClock` |
| `0xC0–0xC2` | **SLE4406/GPM103 prepaid counter card** | data=`0xC0` bit1, clock=`0xC1` bit3, reset=`0xC2` bit3 — matches `cardInput`/`smartOutput` |
| DMA (0x20–0x31) | magnetic-stripe sample feed | |
| `0xE0–0xE7` | external coin-validator UART (MTR2) | matches `CoinUART` |

**Correction note:** an earlier revision of these notes put the SLE4406 card on `0x40–0x42`. That
was wrong — `0x40–0x42` is the **config EEPROM**; the real card is on `0xC0–0xC2` (exactly where this
emulator already models it). The read protocols are what separate them: the EEPROM is an **addressed
Microwire** read (start bit + opcode + word address + 16 bits), the SLE4406 is a **sequential**
bit-stream from reset (no address command).

## Config EEPROM signature (to provision a bootable config)
The firmware validates the EEPROM before use: it reads three 16-bit words and accepts only if they
equal **`0xA5C4`, `0x5A3B`, `0x1F2E`** (at EEPROM word addresses `0x00`, `0x90`, `0x18`). An
emulated EEPROM carrying these passes the config check.

## SLE4406 card acceptance (to provision an accepted card)
Acceptance is **card-type-indexed + internal field-consistency**, not a fixed value table. A card
is accepted iff:
1. its card-family selector = **8** (the SLE4406 path), and
2. its **card-type byte ∈ {`0xD3`, `0xD4`, `0xD5`, `0xD6`, `0xD7`}**, and
3. the per-type field relationship holds:

| card-type | rule (on the parsed issuer record) |
|---|---|
| 0xD3 | `len == 5` and `field7 == 0` |
| 0xD4 | `len == 5` |
| 0xD5 / 0xD6 | `len == field9 + 6` |
| 0xD7 | `len == 6` and `field9 == 1` |

So a valid card needs a self-consistent manufacturer/issuer record of one of those types, not three
planted words. The exact on-card bit offsets of `len`/`field7`/`field9` were not pinned statically
(they resolve through a runtime-computed address chain) — a one-watchpoint trace of the card-read
addresses during a recognition pass pins them directly.

## Confidence
High: CPU, MMU map, CBR board split, far-call ABI, the EEPROM vs card port split and read protocols,
the MTR2 dual-chip flash. Firmware-verified: the EEPROM signature words and the SLE4406 accept rules
(type codes `0xD3–0xD7` + relations). Open: the exact on-card bit offsets of the card's issuer
fields (best resolved dynamically).
