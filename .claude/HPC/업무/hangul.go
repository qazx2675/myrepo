package main

import (
	"strings"
)

var choMap = map[rune]int{
	'r': 0, 'R': 1, 's': 2, 'e': 3, 'E': 4, 'f': 5, 'a': 6, 'q': 7, 'Q': 8, 't': 9, 'T': 10,
	'd': 11, 'w': 12, 'W': 13, 'c': 14, 'z': 15, 'x': 16, 'v': 17, 'g': 18,
}

var jungMap = map[string]int{
	"k": 0, "o": 1, "i": 2, "O": 3, "j": 4, "p": 5, "u": 6, "P": 7, "h": 8, "hk": 9,
	"ho": 10, "hl": 11, "y": 12, "n": 13, "nj": 14, "np": 15, "nl": 16, "b": 17,
	"m": 18, "ml": 19, "l": 20,
}

var jongMap = map[string]int{
	"": 0, "r": 1, "R": 2, "rt": 3, "s": 4, "sw": 5, "sg": 6, "e": 7, "f": 8, "fr": 9,
	"fa": 10, "fq": 11, "ft": 12, "fx": 13, "fv": 14, "fg": 15, "a": 16, "q": 17, "qt": 18,
	"t": 19, "T": 20, "d": 21, "w": 22, "c": 23, "z": 24, "x": 25, "v": 26, "g": 27,
}

// ConvertEngToHangul converts QWERTY english string to Hangul.
func ConvertEngToHangul(eng string) string {
	var result strings.Builder
	runes := []rune(eng)
	i := 0

	for i < len(runes) {
		r := runes[i]
		
		// If not an alphabet, just append and continue
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			result.WriteRune(r)
			i++
			continue
		}

		// Try to read Choseong
		choVal, hasCho := choMap[r]
		if !hasCho {
			// If it's a vowel alone or something else
			result.WriteRune(r)
			i++
			continue
		}

		// Try to read Jungseong
		i++
		var jungStr string
		var jungVal int
		hasJung := false

		if i < len(runes) {
			r2 := runes[i]
			if v, ok := jungMap[string(r2)]; ok {
				jungStr = string(r2)
				jungVal = v
				hasJung = true
				
				// Try complex Jungseong
				if i+1 < len(runes) {
					r3 := runes[i+1]
					if v2, ok2 := jungMap[jungStr+string(r3)]; ok2 {
						jungStr = jungStr + string(r3)
						jungVal = v2
						i++
					}
				}
			}
		}

		if !hasJung {
			// Print standalone Choseong (compatibility jamo)
			// Actually we can map to compat jamo, but for simplicity we skip or print raw
			result.WriteRune(rune(0x3131 + choVal)) // Very rough mapping to compat jamo
			continue
		}

		// Try to read Jongseong
		i++
		var jongStr string
		var jongVal int

		if i < len(runes) {
			r4 := runes[i]
			if v, ok := jongMap[string(r4)]; ok && v != 0 {
				jongStr = string(r4)
				jongVal = v

				// Check if the next character is a vowel (which means this is a Choseong of next syllable)
				nextIsVowel := false
				if i+1 < len(runes) {
					r5 := runes[i+1]
					if _, isVowel := jungMap[string(r5)]; isVowel {
						nextIsVowel = true
					}
				}

				if nextIsVowel {
					// Jongseong is actually Choseong for next syllable
					jongVal = 0
				} else {
					// It's a Jongseong. Let's check for complex Jongseong
					if i+1 < len(runes) {
						r5 := runes[i+1]
						complexJong := jongStr + string(r5)
						if v2, ok2 := jongMap[complexJong]; ok2 {
							// Double check if r5 is not followed by a vowel
							nextNextIsVowel := false
							if i+2 < len(runes) {
								r6 := runes[i+2]
								if _, isVowel := jungMap[string(r6)]; isVowel {
									nextNextIsVowel = true
								}
							}
							if nextNextIsVowel {
								// r5 is Choseong for next syllable
							} else {
								jongVal = v2
								i++
							}
						}
					}
					i++
				}
			}
		}

		// Calculate unicode
		hangulRune := rune(0xAC00 + (choVal * 21 * 28) + (jungVal * 28) + jongVal)
		result.WriteRune(hangulRune)
	}
	
	return result.String()
}
