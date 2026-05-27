package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
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
// WAV synthesis — kept intact, not used in HTTP mode
// ---------------------------------------------------------------------------

const (
	sampleRate          = 6000
	dotLenMs            = 100
	dashLenMs           = dotLenMs * 3
	interElementGapMs   = dotLenMs
	interCharacterGapMs = dotLenMs * 3
	interWordGapMs      = dotLenMs * 7
	freq                = 900.0
	volume              = 0.5
)

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
		if i == len(morseStr)-1 {
			silence(interCharacterGapMs)
		}
	}
	var buf bytes.Buffer
	writeString := func(s string) { buf.WriteString(s) }
	writeString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(36+len(samples)*2))
	writeString("WAVE")
	writeString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint16(1))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&buf, binary.LittleEndian, uint32(sampleRate*2))
	binary.Write(&buf, binary.LittleEndian, uint16(2))
	binary.Write(&buf, binary.LittleEndian, uint16(16))
	writeString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(len(samples)*2))
	for _, s := range samples {
		binary.Write(&buf, binary.LittleEndian, s)
	}
	return buf.Bytes()
}

func playAudio(file string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("afplay", file).Run()
	case "windows":
		return exec.Command("cmd", "/c", "start", "/min", file).Run()
	default:
		if err := exec.Command("aplay", file).Run(); err == nil {
			return nil
		}
		return exec.Command("xdg-open", file).Run()
	}
}

// ---------------------------------------------------------------------------
// CLI entrypoint — kept intact, not used in HTTP mode
// ---------------------------------------------------------------------------

func runCLI() {
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
		time.Sleep(5 * time.Second)
	} else {
		time.Sleep(1 * time.Second)
	}
}

// ---------------------------------------------------------------------------
// HTTP handlers
// ---------------------------------------------------------------------------

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func encodeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		http.Error(w, "invalid request: provide {\"message\": \"your text\"}", http.StatusBadRequest)
		return
	}

	morseStr := textToMorse(req.Message)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"input": req.Message,
		"morse": morseStr,
	})
}

// ---------------------------------------------------------------------------
// main — HTTP server
// ---------------------------------------------------------------------------

func main() {
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/encode", encodeHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("morse-api listening on :%s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}