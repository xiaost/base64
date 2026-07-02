//go:build !purego

// AVX2 base64 kernels, after Wojciech Muła's algorithms
// (http://0x80.pl/notesen/2016-01-12-sse-base64-encoding.html).
//
// Encode: each 32-byte output block consumes 24 input bytes. The input is
// loaded as two 16-byte halves (offsets 0 and 12), each lane shuffled so
// every dword holds one 24-bit group, then mulhi/mullo shifts isolate the
// four 6-bit indices per dword. Translation to ASCII adds a per-range
// offset selected by pshufb from a 16-byte lut.
//
// Decode: each 32-byte input block yields 24 output bytes. Hi/lo nibble
// pshufb masks classify bytes (AND != 0 means invalid: padding, newlines,
// garbage -> stop before consuming the block); a roll lut indexed by hi
// nibble (index 0 for the marker char '/' or '_') converts ASCII to 6-bit
// values; maddubs/madd packs them into 24-bit groups and a shuffle+permd
// compacts the result.

#include "textflag.h"

DATA encShuf<>+0(SB)/8, $0x0405030401020001
DATA encShuf<>+8(SB)/8, $0x0A0B090A07080607
DATA encShuf<>+16(SB)/8, $0x0405030401020001
DATA encShuf<>+24(SB)/8, $0x0A0B090A07080607
GLOBL encShuf<>(SB), RODATA|NOPTR, $32

DATA encConst<>+0(SB)/4, $0x0FC0FC00  // mask for indices 0 and 2
DATA encConst<>+4(SB)/4, $0x04000040  // mulhi shifts
DATA encConst<>+8(SB)/4, $0x003F03F0  // mask for indices 1 and 3
DATA encConst<>+12(SB)/4, $0x01000010 // mullo shifts
DATA encConst<>+16(SB)/1, $51
DATA encConst<>+17(SB)/1, $26
DATA encConst<>+18(SB)/1, $13
GLOBL encConst<>(SB), RODATA|NOPTR, $20

// dword = c0<<18|c1<<12|c2<<6|c3 as LE bytes [b2,b1,b0,0] -> [b0,b1,b2,...]
DATA decShuf<>+0(SB)/8, $0x090A040506000102
DATA decShuf<>+8(SB)/8, $0xFFFFFFFF0C0D0E08
DATA decShuf<>+16(SB)/8, $0x090A040506000102
DATA decShuf<>+24(SB)/8, $0xFFFFFFFF0C0D0E08
GLOBL decShuf<>(SB), RODATA|NOPTR, $32

DATA decPerm<>+0(SB)/4, $0
DATA decPerm<>+4(SB)/4, $1
DATA decPerm<>+8(SB)/4, $2
DATA decPerm<>+12(SB)/4, $4
DATA decPerm<>+16(SB)/4, $5
DATA decPerm<>+20(SB)/4, $6
DATA decPerm<>+24(SB)/4, $7
DATA decPerm<>+28(SB)/4, $7
GLOBL decPerm<>(SB), RODATA|NOPTR, $32

DATA decConst<>+0(SB)/4, $0x01400140 // maddubs: c0*64 + c1
DATA decConst<>+4(SB)/4, $0x00011000 // madd: w0*4096 + w1
DATA decConst<>+8(SB)/1, $0x0F
GLOBL decConst<>(SB), RODATA|NOPTR, $12

// func encodeAVX2(dst, src *byte, blocks int, lut *byte)
// blocks > 0; consumes blocks*24 bytes (reading blocks*24+4),
// writes blocks*32 bytes.
TEXT ·encodeAVX2(SB), NOSPLIT, $0-32
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ blocks+16(FP), CX
	MOVQ lut+24(FP), DX

	VBROADCASTI128 (DX), Y7
	VMOVDQU        encShuf<>(SB), Y6
	VPBROADCASTD   encConst<>+0(SB), Y8
	VPBROADCASTD   encConst<>+4(SB), Y9
	VPBROADCASTD   encConst<>+8(SB), Y10
	VPBROADCASTD   encConst<>+12(SB), Y11
	VPBROADCASTB   encConst<>+16(SB), Y12
	VPBROADCASTB   encConst<>+17(SB), Y13
	VPBROADCASTB   encConst<>+18(SB), Y14

enloop:
	VMOVDQU     (SI), X0
	VINSERTI128 $1, 12(SI), Y0, Y0
	VPSHUFB     Y6, Y0, Y0

	// dword bytes -> [idx0, idx1, idx2, idx3]
	VPAND    Y8, Y0, Y1
	VPMULHUW Y9, Y1, Y1
	VPAND    Y10, Y0, Y2
	VPMULLW  Y11, Y2, Y2
	VPOR     Y2, Y1, Y0

	// ASCII translation: shift = lut[idx<26 ? 13 : satsub(idx,51)]
	VPSUBUSB Y12, Y0, Y1
	VPCMPGTB Y0, Y13, Y2
	VPAND    Y14, Y2, Y2
	VPOR     Y2, Y1, Y1
	VPSHUFB  Y1, Y7, Y1
	VPADDB   Y1, Y0, Y0

	VMOVDQU Y0, (DI)
	ADDQ    $24, SI
	ADDQ    $32, DI
	DECQ    CX
	JNZ     enloop

	VZEROUPPER
	RET

// func encodeAVX2Last(dst, src *byte, lut *byte)
// Like encodeAVX2, but handles one final 24-byte block without reading
// past src+24.
TEXT ·encodeAVX2Last(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ lut+16(FP), DX

	VBROADCASTI128 (DX), Y7
	VMOVDQU        encShuf<>(SB), Y6
	VPBROADCASTD   encConst<>+0(SB), Y8
	VPBROADCASTD   encConst<>+4(SB), Y9
	VPBROADCASTD   encConst<>+8(SB), Y10
	VPBROADCASTD   encConst<>+12(SB), Y11
	VPBROADCASTB   encConst<>+16(SB), Y12
	VPBROADCASTB   encConst<>+17(SB), Y13
	VPBROADCASTB   encConst<>+18(SB), Y14

	VMOVDQU     (SI), X0
	VMOVDQU     8(SI), X1
	VPSRLDQ     $4, X1, X1
	VINSERTI128 $1, X1, Y0, Y0
	VPSHUFB     Y6, Y0, Y0

	// dword bytes -> [idx0, idx1, idx2, idx3]
	VPAND    Y8, Y0, Y1
	VPMULHUW Y9, Y1, Y1
	VPAND    Y10, Y0, Y2
	VPMULLW  Y11, Y2, Y2
	VPOR     Y2, Y1, Y0

	// ASCII translation: shift = lut[idx<26 ? 13 : satsub(idx,51)]
	VPSUBUSB Y12, Y0, Y1
	VPCMPGTB Y0, Y13, Y2
	VPAND    Y14, Y2, Y2
	VPOR     Y2, Y1, Y1
	VPSHUFB  Y1, Y7, Y1
	VPADDB   Y1, Y0, Y0

	VMOVDQU Y0, (DI)
	VZEROUPPER
	RET

// func decodeAVX2(dst, src *byte, blocks int, tab *byte) int
// blocks > 0; returns the number of src bytes consumed (a multiple of 32);
// writes consumed/4*3 bytes to dst.
TEXT ·decodeAVX2(SB), NOSPLIT, $0-40
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ blocks+16(FP), CX
	MOVQ tab+24(FP), DX

	VMOVDQU      (DX), Y10   // lo-nibble mask
	VMOVDQU      32(DX), Y11 // hi-nibble mask
	VMOVDQU      64(DX), Y12 // roll lut
	VMOVDQU      96(DX), Y13 // marker char
	VPBROADCASTD decConst<>+0(SB), Y8
	VPBROADCASTD decConst<>+4(SB), Y7
	VPBROADCASTB decConst<>+8(SB), Y9
	VMOVDQU      decShuf<>(SB), Y6
	VMOVDQU      decPerm<>(SB), Y5
	XORQ         AX, AX

deloop:
	VMOVDQU (SI), Y0
	VPSRLW  $4, Y0, Y1
	VPAND   Y9, Y1, Y1 // hi nibbles
	VPAND   Y9, Y0, Y2 // lo nibbles
	VPSHUFB Y1, Y11, Y3
	VPSHUFB Y2, Y10, Y4
	VPAND   Y4, Y3, Y3
	VPTEST  Y3, Y3
	JNZ     dedone

	// ASCII -> 6-bit values: x += roll[x==marker ? 0 : hi]
	VPCMPEQB Y13, Y0, Y3
	VPANDN   Y1, Y3, Y1
	VPSHUFB  Y1, Y12, Y3
	VPADDB   Y3, Y0, Y0

	// pack 4x6 bits -> 3 bytes per dword, compact to 24 bytes
	VPMADDUBSW Y8, Y0, Y0
	VPMADDWD   Y7, Y0, Y0
	VPSHUFB    Y6, Y0, Y0
	VPERMD     Y0, Y5, Y0

	VMOVDQU      X0, (DI)
	VEXTRACTI128 $1, Y0, X1
	VMOVQ        X1, 16(DI)

	ADDQ $32, SI
	ADDQ $24, DI
	ADDQ $32, AX
	DECQ CX
	JNZ  deloop

dedone:
	VZEROUPPER
	MOVQ AX, ret+32(FP)
	RET

// func encodeSSE(dst, src *byte, blocks int, lut *byte)
// SSSE3 variant of encodeAVX2: blocks > 0; consumes blocks*12 bytes
// (reading blocks*12+4), writes blocks*16 bytes.
TEXT ·encodeSSE(SB), NOSPLIT, $0-32
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ blocks+16(FP), CX
	MOVQ lut+24(FP), DX

	MOVOU  (DX), X7
	MOVOU  encShuf<>(SB), X6
	MOVL   $0x0FC0FC00, AX
	MOVQ   AX, X8
	PSHUFD $0, X8, X8
	MOVL   $0x04000040, AX
	MOVQ   AX, X9
	PSHUFD $0, X9, X9
	MOVL   $0x003F03F0, AX
	MOVQ   AX, X10
	PSHUFD $0, X10, X10
	MOVL   $0x01000010, AX
	MOVQ   AX, X11
	PSHUFD $0, X11, X11
	MOVL   $0x33333333, AX
	MOVQ   AX, X12
	PSHUFD $0, X12, X12 // 51 in every byte
	MOVL   $0x1A1A1A1A, AX
	MOVQ   AX, X13
	PSHUFD $0, X13, X13 // 26 in every byte
	MOVL   $0x0D0D0D0D, AX
	MOVQ   AX, X14
	PSHUFD $0, X14, X14 // 13 in every byte

ensloop:
	MOVOU  (SI), X0
	PSHUFB X6, X0

	// dword bytes -> [idx0, idx1, idx2, idx3]
	MOVOU   X0, X1
	PAND    X8, X1
	PMULHUW X9, X1
	PAND    X10, X0
	PMULLW  X11, X0
	POR     X1, X0

	// ASCII translation: shift = lut[idx<26 ? 13 : satsub(idx,51)]
	MOVOU   X0, X1
	PSUBUSB X12, X1
	MOVOU   X13, X2
	PCMPGTB X0, X2
	PAND    X14, X2
	POR     X2, X1
	MOVOU   X7, X2
	PSHUFB  X1, X2
	PADDB   X2, X0

	MOVOU X0, (DI)
	ADDQ  $12, SI
	ADDQ  $16, DI
	DECQ  CX
	JNZ   ensloop
	RET

// func encodeSSELast(dst, src *byte, lut *byte)
// Like encodeSSE, but handles one final 12-byte block without reading
// past src+12.
TEXT ·encodeSSELast(SB), NOSPLIT, $0-24
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ lut+16(FP), DX

	MOVOU  (DX), X7
	MOVOU  encShuf<>(SB), X6
	MOVL   $0x0FC0FC00, AX
	MOVQ   AX, X8
	PSHUFD $0, X8, X8
	MOVL   $0x04000040, AX
	MOVQ   AX, X9
	PSHUFD $0, X9, X9
	MOVL   $0x003F03F0, AX
	MOVQ   AX, X10
	PSHUFD $0, X10, X10
	MOVL   $0x01000010, AX
	MOVQ   AX, X11
	PSHUFD $0, X11, X11
	MOVL   $0x33333333, AX
	MOVQ   AX, X12
	PSHUFD $0, X12, X12 // 51 in every byte
	MOVL   $0x1A1A1A1A, AX
	MOVQ   AX, X13
	PSHUFD $0, X13, X13 // 26 in every byte
	MOVL   $0x0D0D0D0D, AX
	MOVQ   AX, X14
	PSHUFD $0, X14, X14 // 13 in every byte

	MOVQ   (SI), X0
	MOVQ   4(SI), X1
	PSRLDQ $4, X1
	PSLLDQ $8, X1
	POR    X1, X0
	PSHUFB X6, X0

	// dword bytes -> [idx0, idx1, idx2, idx3]
	MOVOU   X0, X1
	PAND    X8, X1
	PMULHUW X9, X1
	PAND    X10, X0
	PMULLW  X11, X0
	POR     X1, X0

	// ASCII translation: shift = lut[idx<26 ? 13 : satsub(idx,51)]
	MOVOU   X0, X1
	PSUBUSB X12, X1
	MOVOU   X13, X2
	PCMPGTB X0, X2
	PAND    X14, X2
	POR     X2, X1
	MOVOU   X7, X2
	PSHUFB  X1, X2
	PADDB   X2, X0

	MOVOU X0, (DI)
	RET

// func decodeSSE(dst, src *byte, blocks int, tab *byte) int
// SSSE3 variant of decodeAVX2: blocks > 0; returns the number of src
// bytes consumed (a multiple of 16); writes consumed/4*3 bytes to dst.
TEXT ·decodeSSE(SB), NOSPLIT, $0-40
	MOVQ dst+0(FP), DI
	MOVQ src+8(FP), SI
	MOVQ blocks+16(FP), CX
	MOVQ tab+24(FP), DX

	MOVOU  (DX), X10   // lo-nibble mask
	MOVOU  32(DX), X11 // hi-nibble mask
	MOVOU  64(DX), X12 // roll lut
	MOVOU  96(DX), X13 // marker char
	MOVL   $0x01400140, AX
	MOVQ   AX, X8
	PSHUFD $0, X8, X8
	MOVL   $0x00011000, AX
	MOVQ   AX, X7
	PSHUFD $0, X7, X7
	MOVL   $0x0F0F0F0F, AX
	MOVQ   AX, X9
	PSHUFD $0, X9, X9
	MOVOU  decShuf<>(SB), X6
	PXOR   X5, X5
	XORQ   AX, AX

desloop:
	MOVOU (SI), X0
	MOVOU X0, X1
	PSRLW $4, X1
	PAND  X9, X1 // hi nibbles
	MOVOU X0, X2
	PAND  X9, X2 // lo nibbles

	MOVOU   X11, X3
	PSHUFB  X1, X3
	MOVOU   X10, X4
	PSHUFB  X2, X4
	PAND    X4, X3
	PCMPEQB X5, X3 // 0xFF where valid
	PMOVMSKB X3, BX
	CMPL    BX, $0xFFFF
	JNE     desdone

	// ASCII -> 6-bit values: x += roll[x==marker ? 0 : hi]
	MOVOU   X13, X3
	PCMPEQB X0, X3
	PANDN   X1, X3
	MOVOU   X12, X4
	PSHUFB  X3, X4
	PADDB   X4, X0

	// pack 4x6 bits -> 3 bytes per dword, compact to 12 bytes
	PMADDUBSW X8, X0
	PMADDWL   X7, X0
	PSHUFB    X6, X0

	MOVQ   X0, (DI)
	PSRLDQ $4, X0
	MOVQ   X0, 4(DI) // overlapping store: bytes 4..11

	ADDQ $16, SI
	ADDQ $12, DI
	ADDQ $16, AX
	DECQ CX
	JNZ  desloop

desdone:
	MOVQ AX, ret+32(FP)
	RET

// func cpuidex(eaxArg, ecxArg uint32) (eax, ebx, ecx, edx uint32)
TEXT ·cpuidex(SB), NOSPLIT, $0-24
	MOVL eaxArg+0(FP), AX
	MOVL ecxArg+4(FP), CX
	CPUID
	MOVL AX, eax+8(FP)
	MOVL BX, ebx+12(FP)
	MOVL CX, ecx+16(FP)
	MOVL DX, edx+20(FP)
	RET

// func xgetbv0() (eax, edx uint32)
TEXT ·xgetbv0(SB), NOSPLIT, $0-8
	XORL CX, CX
	XGETBV
	MOVL AX, eax+0(FP)
	MOVL DX, edx+4(FP)
	RET
