//go:build !purego

// NEON base64 kernels, after the design of aklomp/base64.
//
// Encode: LD3 de-interleaves 48 input bytes into the 3 bytes of each
// 24-bit group; shifts/shift-inserts build four 6-bit index vectors; TBL
// over the 64-byte alphabet maps them to ASCII; ST4 interleaves the 4
// output chars. A trailing 24-byte block repeats this on 8-byte lanes.
//
// Decode (custom alphabets): LD4 de-interleaves 64 input chars; the
// 256-byte LUT (invalid = 0xFF) is applied as a TBL plus three chained
// TBX over 64-byte chunks (TBX keeps the destination lane when the index
// is out of range, so the chain reconstructs the full 256-byte lookup);
// any byte >= 0x40 in the result marks an invalid char and stops the loop
// before the block is consumed; shift-inserts repack 6-bit values and ST3
// interleaves the 3 output bytes.
//
// Decode (std/URL alphabets): same LD4/repack/ST3 frame, but chars are
// classified by hi/lo-nibble pshufb-style masks (Muła's scheme, one AND
// selecting invalid bytes) and translated by adding roll[hi nibble],
// where the marker char ('/' or '_') is steered to roll[0] with CMEQ+BIT.
// Trailing 16-char blocks are decoded in-order, packing the four quanta
// inside each dword and compacting with TBL.

#include "textflag.h"

// In-order 16-char tail blocks pack each dword's 24-bit group as LE bytes
// [b2,b1,b0,junk]; this shuffle reverses and compacts them to 12 bytes.
DATA decShufN<>+0(SB)/8, $0x090A040506000102
DATA decShufN<>+8(SB)/8, $0xFFFFFFFF0C0D0E08
GLOBL decShufN<>(SB), RODATA|NOPTR, $16

#define DECODE_NIBBLE(IN, OUT, BAD, HI, LO, TMP) \
	VUSHR $4, IN.B16, HI.B16 \
	VAND  V31.B16, IN.B16, LO.B16 \
	VTBL  LO.B16, [V8.B16], BAD.B16 \
	VTBL  HI.B16, [V9.B16], TMP.B16 \
	VAND  TMP.B16, BAD.B16, BAD.B16 \
	VCMEQ V11.B16, IN.B16, TMP.B16 \
	VBIT  TMP.B16, V12.B16, HI.B16 \
	VTBL  HI.B16, [V10.B16], OUT.B16 \
	VADD  OUT.B16, IN.B16, OUT.B16

// func encodeNEON(dst, src *byte, n int, lut *byte)
// n > 0 and a multiple of 24; writes n/3*4 bytes to dst.
// Whole 48-byte blocks use the full-width loop; a trailing 24-byte
// block runs the same steps once on 8-byte arrangements.
TEXT ·encodeNEON(SB), NOSPLIT, $0-32
	MOVD dst+0(FP), R0
	MOVD src+8(FP), R1
	MOVD n+16(FP), R2
	MOVD lut+24(FP), R3

	VLD1 (R3), [V8.B16, V9.B16, V10.B16, V11.B16]
	MOVD $0x3F, R4
	VDUP R4, V12.B16

	SUBS $48, R2, R2
	BLT  entail

enloop:
	VLD3.P 48(R1), [V0.B16, V1.B16, V2.B16]

	// idx0 = b0 >> 2
	VUSHR $2, V0.B16, V4.B16
	// idx1 = ((b0 << 4) | (b1 >> 4)) & 0x3F
	VUSHR $4, V1.B16, V5.B16
	VSLI  $4, V0.B16, V5.B16
	VAND  V12.B16, V5.B16, V5.B16
	// idx2 = ((b1 << 2) | (b2 >> 6)) & 0x3F
	VUSHR $6, V2.B16, V6.B16
	VSLI  $2, V1.B16, V6.B16
	VAND  V12.B16, V6.B16, V6.B16
	// idx3 = b2 & 0x3F
	VAND  V12.B16, V2.B16, V7.B16

	VTBL V4.B16, [V8.B16, V9.B16, V10.B16, V11.B16], V16.B16
	VTBL V5.B16, [V8.B16, V9.B16, V10.B16, V11.B16], V17.B16
	VTBL V6.B16, [V8.B16, V9.B16, V10.B16, V11.B16], V18.B16
	VTBL V7.B16, [V8.B16, V9.B16, V10.B16, V11.B16], V19.B16

	VST4.P [V16.B16, V17.B16, V18.B16, V19.B16], 64(R0)

	SUBS $48, R2, R2
	BGE  enloop

entail:
	ADDS $48, R2, R2
	BEQ  endone

	VLD3 (R1), [V0.B8, V1.B8, V2.B8]

	VUSHR $2, V0.B8, V4.B8
	VUSHR $4, V1.B8, V5.B8
	VSLI  $4, V0.B8, V5.B8
	VAND  V12.B8, V5.B8, V5.B8
	VUSHR $6, V2.B8, V6.B8
	VSLI  $2, V1.B8, V6.B8
	VAND  V12.B8, V6.B8, V6.B8
	VAND  V12.B8, V2.B8, V7.B8

	VTBL V4.B8, [V8.B16, V9.B16, V10.B16, V11.B16], V16.B8
	VTBL V5.B8, [V8.B16, V9.B16, V10.B16, V11.B16], V17.B8
	VTBL V6.B8, [V8.B16, V9.B16, V10.B16, V11.B16], V18.B8
	VTBL V7.B8, [V8.B16, V9.B16, V10.B16, V11.B16], V19.B8

	VST4 [V16.B8, V17.B8, V18.B8, V19.B8], (R0)

endone:
	RET

// func decodeNEON(dst, src *byte, n int, lut *byte) int
// n > 0 and a multiple of 64; returns the number of src bytes consumed
// (a multiple of 64); writes consumed/4*3 bytes to dst.
TEXT ·decodeNEON(SB), NOSPLIT, $0-40
	MOVD dst+0(FP), R0
	MOVD src+8(FP), R1
	MOVD n+16(FP), R2
	MOVD lut+24(FP), R3

	VLD1.P 64(R3), [V8.B16, V9.B16, V10.B16, V11.B16]
	VLD1.P 64(R3), [V12.B16, V13.B16, V14.B16, V15.B16]
	VLD1.P 64(R3), [V16.B16, V17.B16, V18.B16, V19.B16]
	VLD1   (R3), [V20.B16, V21.B16, V22.B16, V23.B16]
	MOVD   $64, R5
	VDUP   R5, V24.B16
	MOVD   $0, R4

deloop:
	VLD4.P 64(R1), [V0.B16, V1.B16, V2.B16, V3.B16]

	// V4 = lut[V0]
	VTBL V0.B16, [V8.B16, V9.B16, V10.B16, V11.B16], V4.B16
	VSUB V24.B16, V0.B16, V25.B16
	VTBX V25.B16, [V12.B16, V13.B16, V14.B16, V15.B16], V4.B16
	VSUB V24.B16, V25.B16, V25.B16
	VTBX V25.B16, [V16.B16, V17.B16, V18.B16, V19.B16], V4.B16
	VSUB V24.B16, V25.B16, V25.B16
	VTBX V25.B16, [V20.B16, V21.B16, V22.B16, V23.B16], V4.B16

	// V5 = lut[V1]
	VTBL V1.B16, [V8.B16, V9.B16, V10.B16, V11.B16], V5.B16
	VSUB V24.B16, V1.B16, V25.B16
	VTBX V25.B16, [V12.B16, V13.B16, V14.B16, V15.B16], V5.B16
	VSUB V24.B16, V25.B16, V25.B16
	VTBX V25.B16, [V16.B16, V17.B16, V18.B16, V19.B16], V5.B16
	VSUB V24.B16, V25.B16, V25.B16
	VTBX V25.B16, [V20.B16, V21.B16, V22.B16, V23.B16], V5.B16

	// V6 = lut[V2]
	VTBL V2.B16, [V8.B16, V9.B16, V10.B16, V11.B16], V6.B16
	VSUB V24.B16, V2.B16, V25.B16
	VTBX V25.B16, [V12.B16, V13.B16, V14.B16, V15.B16], V6.B16
	VSUB V24.B16, V25.B16, V25.B16
	VTBX V25.B16, [V16.B16, V17.B16, V18.B16, V19.B16], V6.B16
	VSUB V24.B16, V25.B16, V25.B16
	VTBX V25.B16, [V20.B16, V21.B16, V22.B16, V23.B16], V6.B16

	// V30 = lut[V3]
	VTBL V3.B16, [V8.B16, V9.B16, V10.B16, V11.B16], V30.B16
	VSUB V24.B16, V3.B16, V25.B16
	VTBX V25.B16, [V12.B16, V13.B16, V14.B16, V15.B16], V30.B16
	VSUB V24.B16, V25.B16, V25.B16
	VTBX V25.B16, [V16.B16, V17.B16, V18.B16, V19.B16], V30.B16
	VSUB V24.B16, V25.B16, V25.B16
	VTBX V25.B16, [V20.B16, V21.B16, V22.B16, V23.B16], V30.B16

	// Any value >= 0x40 (0xFF entries included) means an invalid char.
	VORR  V5.B16, V4.B16, V27.B16
	VORR  V6.B16, V27.B16, V27.B16
	VORR  V30.B16, V27.B16, V27.B16
	VUSHR $6, V27.B16, V27.B16
	VMOV  V27.D[0], R5
	VMOV  V27.D[1], R6
	ORR   R6, R5, R5
	CBNZ  R5, dedone

	// out0 = v0<<2 | v1>>4; out1 = v1<<4 | v2>>2; out2 = v2<<6 | v3
	VUSHR $4, V5.B16, V28.B16
	VSLI  $2, V4.B16, V28.B16
	VUSHR $2, V6.B16, V29.B16
	VSLI  $4, V5.B16, V29.B16
	VSLI  $6, V6.B16, V30.B16

	VST3.P [V28.B16, V29.B16, V30.B16], 48(R0)

	ADD  $64, R4
	SUBS $64, R2, R2
	BGT  deloop

dedone:
	MOVD R4, ret+32(FP)
	RET

// func decodeNEONFast(dst, src *byte, n int, tab *byte) int
// n > 0 and a multiple of 16; tab is lo/hi/roll/marker vectors.
// Whole 64-char blocks use the deinterleaved LD4 loop; trailing 16-char
// blocks are decoded in-order, packing quanta inside each dword.
TEXT ·decodeNEONFast(SB), NOSPLIT, $0-40
	MOVD dst+0(FP), R0
	MOVD src+8(FP), R1
	MOVD n+16(FP), R2
	MOVD tab+24(FP), R3

	VLD1.P 16(R3), [V8.B16]  // lo-nibble invalid mask
	VLD1.P 16(R3), [V9.B16]  // hi-nibble invalid mask
	VLD1.P 16(R3), [V10.B16] // roll lut
	VLD1   (R3), [V11.B16]   // marker char
	VEOR   V12.B16, V12.B16, V12.B16
	MOVD   $0x0F, R5
	VDUP   R5, V31.B16
	MOVD   $0, R4

	SUBS $64, R2, R2
	BLT  dftail

dfloop:
	VLD4.P 64(R1), [V0.B16, V1.B16, V2.B16, V3.B16]

	DECODE_NIBBLE(V0, V4, V20, V13, V14, V15)
	DECODE_NIBBLE(V1, V5, V21, V13, V14, V15)
	DECODE_NIBBLE(V2, V6, V22, V13, V14, V15)
	DECODE_NIBBLE(V3, V30, V23, V13, V14, V15)

	VORR V21.B16, V20.B16, V24.B16
	VORR V22.B16, V24.B16, V24.B16
	VORR V23.B16, V24.B16, V24.B16
	VMOV V24.D[0], R5
	VMOV V24.D[1], R6
	ORR  R6, R5, R5
	CBNZ R5, dfdone

	// out0 = v0<<2 | v1>>4; out1 = v1<<4 | v2>>2; out2 = v2<<6 | v3
	VUSHR $4, V5.B16, V28.B16
	VSLI  $2, V4.B16, V28.B16
	VUSHR $2, V6.B16, V29.B16
	VSLI  $4, V5.B16, V29.B16
	VSLI  $6, V6.B16, V30.B16

	VST3.P [V28.B16, V29.B16, V30.B16], 48(R0)

	ADD  $64, R4
	SUBS $64, R2, R2
	BGE  dfloop

dftail:
	ADDS $64, R2, R2
	BEQ  dfdone
	MOVD $decShufN<>(SB), R5
	VLD1 (R5), [V26.B16]

dftloop:
	VLD1.P 16(R1), [V0.B16]

	DECODE_NIBBLE(V0, V4, V20, V13, V14, V15)

	VMOV V20.D[0], R5
	VMOV V20.D[1], R6
	ORR  R6, R5, R5
	CBNZ R5, dfdone

	// Quanta are in dword lanes [c0,c1,c2,c3]; pack pairs into 12-bit
	// halves, then halves into a 24-bit group (junk above each field).
	VUSHR $8, V4.H8, V5.H8
	VSLI  $6, V4.H8, V5.H8  // c_even<<6 | c_odd
	VUSHR $16, V5.S4, V6.S4
	VSLI  $12, V5.S4, V6.S4 // c0<<18 | c1<<12 | c2<<6 | c3
	VTBL  V26.B16, [V6.B16], V7.B16

	VST1.P [V7.B8], 8(R0)
	VST1.P V7.S[2], 4(R0)

	ADD  $16, R4
	SUBS $16, R2, R2
	BGT  dftloop

dfdone:
	MOVD R4, ret+32(FP)
	RET

#undef DECODE_NIBBLE
