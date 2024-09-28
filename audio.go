package main

import (
	"log"
	"math"

	"github.com/gordonklaus/portaudio"
)

const sampleRate = 44100
const framesPerBuffer = 256
const threshold = 0.15 // 音量のしきい値

func doAudioLoop() chan struct{} {
	evChan := make(chan struct{}, 10)
	go func() {
		audioLoop(evChan)
		close(evChan)
	}()
	return evChan
}

func audioLoop(evChan chan struct{}) {
	// PortAudioを初期化
	portaudio.Initialize()
	defer portaudio.Terminate()

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

	for {
		// 音声データを取得
		err := stream.Read()
		if err != nil {
			log.Fatalf("Failed to read from stream: %v", err)
		}

		// 音量を計算してしきい値を超えているか判定
		volume := calculateVolume(in)
		if volume > threshold {
			go func() {
				evChan <- struct{}{}
			}()
			// fmt.Printf("Sound detected! Volume: %f\n", volume)
		}
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
