package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Morse lookup table
// ---------------------------------------------------------------------------

var morse = map[rune]string{
	'A': ".-", 'B': "-...", 'C': "-.-.", 'D': "-..",
	'E': ".", 'F': "..-.", 'G': "--.", 'H': "....",
	'I': "..", 'J': ".---", 'K': "-.-", 'L': ".-..",
	'M': "--", 'N': "-.", 'O': "---", 'P': ".--.",
	'Q': "--.-", 'R': ".-.", 'S': "...", 'T': "-",
	'U': "..-", 'V': "...-", 'W': ".--", 'X': "-..-",
	'Y': "-.--", 'Z': "--..",
	'0': "-----", '1': ".----", '2': "..---", '3': "...--",
	'4': "....-", '5': ".....", '6': "-....", '7': "--...",
	'8': "---..", '9': "----.",
	'.': ".-.-.-", ',': "--..--", '?': "..--..", '\'': ".----.",
	'!': "-.-.--", '/': "-..-.", '(': "-.--.", ')': "-.--.-",
	'&': ".-...", ':': "---...", ';': "-.-.-.", '=': "-...-",
	'+': ".-.-.", '-': "-....-", '_': "..--.-", '"': ".-..-.",
	'$': "...-..-", '@': ".--.-.",
}

// translate normal text → Morse, with spaces between letters and “/” between words.
func textToMorse(s string) string {
	var b strings.Builder
	words := strings.Fields(s)
	for wi, w := range words {
		for ci, ch := range w {
			code, ok := morse[unicodeToKey(ch)]
			if ok {
				if ci > 0 {
					b.WriteRune(' ')
				}
				b.WriteString(code)
			}
		}
		if wi < len(words)-1 {
			b.WriteString(" / ")
		}
	}
	return b.String()
}

func unicodeToKey(r rune) rune { return []rune(strings.ToUpper(string(r)))[0] }

// ---------------------------------------------------------------------------
// WAV synthesis (700 Hz sine, 100 ms dit length, 8 kHz sample rate, mono 16-bit)
// ---------------------------------------------------------------------------

const (
	sampleRate          = 6000 // Hz
	dotLenMs            = 100  // dit length
	dashLenMs           = dotLenMs * 3
	interElementGapMs   = dotLenMs     // silence between elements of same char
	interCharacterGapMs = dotLenMs * 3 // silence between letters
	interWordGapMs      = dotLenMs * 7 // silence between words
	freq                = 900.0        // tone frequency
	volume              = 0.5          // 0.0–1.0
)

// buildWAV encodes a WAV file for the given Morse string and returns it as bytes.
func buildWAV(morseStr string) []byte {
	samples := make([]int16, 0, 44100)

	tone := func(durationMs int) {
		count := durationMs * sampleRate / 1000
		for i := 0; i < count; i++ {
			val := volume * math.Sin(2*math.Pi*freq*float64(i)/sampleRate)
			samples = append(samples, int16(val*32767))
		}
	}
	silence := func(durationMs int) {
		count := durationMs * sampleRate / 1000
		samples = append(samples, make([]int16, count)...)
	}

	for i, ch := range morseStr {
		switch ch {
		case '.':
			tone(dotLenMs)
			silence(interElementGapMs)
		case '-':
			tone(dashLenMs)
			silence(interElementGapMs)
		case ' ':
			silence(interCharacterGapMs)
		case '/':
			silence(interWordGapMs)
		}
		// Gap after whole message to avoid abrupt stop
		if i == len(morseStr)-1 {
			silence(interCharacterGapMs)
		}
	}

	var buf bytes.Buffer
	writeString := func(s string) { buf.WriteString(s) }

	// RIFF header ------------------------------------------------------------
	writeString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+len(samples)*2))
	writeString("WAVE")

	// fmt chunk --------------------------------------------------------------
	writeString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16)) // Subchunk1Size
	binary.Write(&buf, binary.LittleEndian, uint16(1))  // PCM
	binary.Write(&buf, binary.LittleEndian, uint16(1))  // Mono
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2)) // ByteRate
	binary.Write(&buf, binary.LittleEndian, uint16(2))            // BlockAlign
	binary.Write(&buf, binary.LittleEndian, uint16(16))           // BitsPerSample

	// data chunk -------------------------------------------------------------
	writeString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(len(samples)*2))
	for _, s := range samples {
		binary.Write(&buf, binary.LittleEndian, s)
	}
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// Cross-platform file playback helpers
// ---------------------------------------------------------------------------

func playAudio(file string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("afplay", file).Run()
	case "windows":
		// '/min' keeps the player window unobtrusive.
		return exec.Command("cmd", "/c", "start", "/min", file).Run()
	default: // Linux, etc.
		// Try aplay first, fall back to xdg-open.
		if err := exec.Command("aplay", file).Run(); err == nil {
			return nil
		}
		return exec.Command("xdg-open", file).Run()
	}
}

// ---------------------------------------------------------------------------
// main
// ---------------------------------------------------------------------------

func main() {
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Enter message to encode: ")
	userMsg, _ := reader.ReadString('\n')
	userMsg = strings.TrimSpace(userMsg)

	if userMsg == "" {
		fmt.Println("No input; exiting.")
		return
	}

	morseStr := textToMorse(userMsg)
	fmt.Println("\nMorse code:")
	fmt.Println(morseStr)

	fmt.Println("\nGenerating audio…")
	wavBytes := buildWAV(morseStr)

	tmpFile, err := os.CreateTemp("", "morse-*.wav")
	if err != nil {
		log.Fatal("temp file:", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(wavBytes); err != nil {
		log.Fatal("write wav:", err)
	}
	tmpFile.Close()

	fmt.Println("Playing audio…")
	if err := playAudio(tmpFile.Name()); err != nil {
		fmt.Printf("Could not play audio automatically (%v).\n", err)
		fmt.Printf("The WAV file is at: %s\n", tmpFile.Name())
		// Keep file around a bit so user can open it.
		time.Sleep(5 * time.Second)
	} else {
		// Give the player time to grab the file before it’s deleted.
		time.Sleep(1 * time.Second)
	}
}
