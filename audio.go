package main

import (
	"fmt"
	"log"
	"math"

	"github.com/gordonklaus/portaudio"
)

const sampleRate = 44100
const framesPerBuffer = 256
const threshold = 0.1 // 音量のしきい値

func main() {
	// PortAudioを初期化
	portaudio.Initialize()
	defer portaudio.Terminate()

	// マイク入力用のストリームを作成
	in := make([]float32, framesPerBuffer)
	stream, err := portaudio.OpenDefaultStream(1, 0, sampleRate, len(in), in)
	if err != nil {
		log.Fatalf("Failed to open stream: %v", err)
	}
	defer stream.Close()

	// ストリームを開始
	err = stream.Start()
	if err != nil {
		log.Fatalf("Failed to start stream: %v", err)
	}
	defer stream.Stop()

	fmt.Println("Listening for sound...")

	for {
		// 音声データを取得
		err := stream.Read()
		if err != nil {
			log.Fatalf("Failed to read from stream: %v", err)
		}

		// 音量を計算してしきい値を超えているか判定
		volume := calculateVolume(in)
		if volume > threshold {
			fmt.Printf("Sound detected! Volume: %f\n", volume)
		}

		// time.Sleep(100 * time.Millisecond) // 100ms間隔でチェック
	}
}

// 音声データから音量（RMS値）を計算
func calculateVolume(samples []float32) float64 {
	var sumSquares float64
	for _, sample := range samples {
		sumSquares += float64(sample * sample)
	}
	return math.Sqrt(sumSquares / float64(len(samples)))
}
