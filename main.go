// Copyright 2018 The Ebiten Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	_ "embed"
	"flag"
	"fmt"
	"image"
	"image/color"
	_ "image/png"
	"log"
	"math"
	"math/rand/v2"
	"sync"
	"time"

	raudio "github.com/Watakumi/flappy-voice/resources/audio"
	resources "github.com/Watakumi/flappy-voice/resources/images"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/wav"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/examples/resources/fonts"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

var flagCRT = flag.Bool("crt", false, "enable the CRT effect")

//go:embed crt.go
var crtGo []byte

func floorDiv(x, y int) int {
	d := x / y
	if d*y == x || x >= 0 {
		return d
	}
	return d - 1
}

func floorMod(x, y int) int {
	return x - floorDiv(x, y)*y
}

const (
	screenWidth      = 640
	screenHeight     = 480
	tileSize         = 32
	titleFontSize    = fontSize * 1.5
	fontSize         = 24
	smallFontSize    = fontSize / 2
	pipeWidth        = tileSize * 2
	pipeStartOffsetX = 8
	pipeIntervalX    = 8
	pipeGapY         = 5
)

var (
	endImage         *ebiten.Image
	tilesImage       *ebiten.Image
	arcadeFaceSource *text.GoTextFaceSource
)

func init() {
	img, _, err := image.Decode(bytes.NewReader(resources.End_png))
	if err != nil {
		log.Fatal(err)
	}
	endImage = ebiten.NewImageFromImage(img)

	img, _, err = image.Decode(bytes.NewReader(resources.Tiles_png))
	if err != nil {
		log.Fatal(err)
	}
	tilesImage = ebiten.NewImageFromImage(img)
}

func init() {
	s, err := text.NewGoTextFaceSource(bytes.NewReader(fonts.PressStart2P_ttf))
	if err != nil {
		log.Fatal(err)
	}
	arcadeFaceSource = s
}

type Mode int
type Difficulty int

const (
	ModeTitle Mode = iota
	ModeGame
	ModeGameOver
)

const (
	NotSelected Difficulty = iota
	Easy
	Normal
	Hard
	End
)

type Game struct {
	mode Mode

	// The end's position
	x16  int
	y16  int
	vy16 int

	// Camera
	cameraX int
	cameraY int

	// Pipes
	pipeTileYs []int

	gameoverCount int

	selectedDifficulty Difficulty

	touchIDs   []ebiten.TouchID
	gamepadIDs []ebiten.GamepadID

	audioContext *audio.Context
	jumpPlayer   *audio.Player
	hitPlayer    *audio.Player

	micChan        chan struct{}
	lastMicEventAt time.Time
	micMu          sync.Mutex
}

func NewGame(crt bool) ebiten.Game {
	g := &Game{}
	g.init()
	if crt {
		return &GameWithCRTEffect{Game: g}
	}
	return g
}

func (g *Game) init() {
	g.x16 = 0
	g.y16 = 100 * 16
	g.cameraX = -240
	g.cameraY = 0
	g.pipeTileYs = make([]int, 256)
	for i := range g.pipeTileYs {
		g.pipeTileYs[i] = rand.IntN(6) + 2
	}

	if g.audioContext == nil {
		g.audioContext = audio.NewContext(48000)
	}

	jumpD, err := wav.DecodeF32(bytes.NewReader(raudio.Jump_ogg))
	if err != nil {
		log.Fatal(err)
	}
	g.jumpPlayer, err = g.audioContext.NewPlayerF32(jumpD)
	if err != nil {
		log.Fatal(err)
	}

	jabD, err := wav.DecodeF32(bytes.NewReader(raudio.Jab_wav))
	if err != nil {
		log.Fatal(err)
	}
	g.hitPlayer, err = g.audioContext.NewPlayerF32(jabD)
	if err != nil {
		log.Fatal(err)
	}

	g.micChan = doAudioLoop()
}

func (g *Game) detectMicEvent() bool {
	g.micMu.Lock()
	defer g.micMu.Unlock()

	now := time.Now()
	if now.Sub(g.lastMicEventAt) > 500*time.Millisecond {
		g.lastMicEventAt = now
		return true
	}
	return false
}

func (g *Game) isKeyJustPressed() bool {
	select {
	case <-g.micChan:
		if g.detectMicEvent() {
			return true
		}
	default:
	}

	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		g.lastMicEventAt = time.Now()
		return true
	}
	if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
		g.lastMicEventAt = time.Now()
		return true
	}
	g.touchIDs = inpututil.AppendJustPressedTouchIDs(g.touchIDs[:0])
	if len(g.touchIDs) > 0 {
		g.lastMicEventAt = time.Now()
		return true
	}
	g.gamepadIDs = ebiten.AppendGamepadIDs(g.gamepadIDs[:0])
	for _, g := range g.gamepadIDs {
		if ebiten.IsStandardGamepadLayoutAvailable(g) {
			if inpututil.IsStandardGamepadButtonJustPressed(g, ebiten.StandardGamepadButtonRightBottom) {
				return true
			}
			if inpututil.IsStandardGamepadButtonJustPressed(g, ebiten.StandardGamepadButtonRightRight) {
				return true
			}
		} else {
			// The button 0/1 might not be A/B buttons.
			if inpututil.IsGamepadButtonJustPressed(g, ebiten.GamepadButton0) {
				return true
			}
			if inpututil.IsGamepadButtonJustPressed(g, ebiten.GamepadButton1) {
				return true
			}
		}
	}
	return false
}

func (g *Game) isSelectedKeyPressed() Difficulty {
	if inpututil.IsKeyJustPressed(ebiten.KeyDigit0) {
		return Easy
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDigit1) {
		return Normal
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyDigit2) {
		return Hard
	}
	return NotSelected
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func (g *Game) Update() error {
	switch g.mode {
	case ModeTitle:
		if g.isSelectedKeyPressed() == NotSelected {
			return nil
		} else {
			g.selectedDifficulty = g.isSelectedKeyPressed()
			g.mode = ModeGame
		}
	case ModeGame:
		g.x16 += 32
		g.cameraX += 2
		if g.isKeyJustPressed() {
			g.vy16 = -96
			if err := g.jumpPlayer.Rewind(); err != nil {
				return err
			}
			g.jumpPlayer.Play()
		}
		g.y16 += g.vy16

		// Gravity
		switch g.selectedDifficulty {
		case Easy:
			g.vy16 += 4
		case Normal:
			g.vy16 += 8
		case Hard:
			g.vy16 += 20
		default:
			g.vy16 += 4
		}

		if g.vy16 > 96 {
			g.vy16 = 96
		}

		if g.hit() {
			if err := g.hitPlayer.Rewind(); err != nil {
				return err
			}
			g.hitPlayer.Play()
			g.mode = ModeGameOver
			g.gameoverCount = 30
		}
	case ModeGameOver:
		if g.gameoverCount > 0 {
			g.gameoverCount--
		}
		if g.gameoverCount == 0 && g.isKeyJustPressed() {
			g.init()
			g.mode = ModeTitle
		}
	}
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{0x80, 0xa0, 0xc0, 0xff})
	g.drawTiles(screen)
	if g.mode != ModeTitle {
		g.drawEnd(screen)
	}

	var titleTexts string
	var texts string
	switch g.mode {
	case ModeTitle:
		titleTexts = "FLAPPY VOICE(仮)"
		texts = `



SELECT MODE

0 = EASY
1 = NORMAL
2 = HARD

PRESS ON SPACE OR SHOUT!
`

	case ModeGameOver:
		texts = "\nGAME OVER!"
	}

	op := &text.DrawOptions{}
	op.GeoM.Translate(screenWidth/2, 3*titleFontSize)
	op.ColorScale.ScaleWithColor(color.White)
	op.LineSpacing = titleFontSize
	op.PrimaryAlign = text.AlignCenter
	text.Draw(screen, titleTexts, &text.GoTextFace{
		Source: arcadeFaceSource,
		Size:   titleFontSize,
	}, op)

	op = &text.DrawOptions{}
	op.GeoM.Translate(screenWidth/2, 3*titleFontSize)
	op.ColorScale.ScaleWithColor(color.White)
	op.LineSpacing = fontSize
	op.PrimaryAlign = text.AlignCenter
	text.Draw(screen, texts, &text.GoTextFace{
		Source: arcadeFaceSource,
		Size:   fontSize,
	}, op)

	if g.mode == ModeTitle {
		const msg = "Special Thanks: END(@aiandrox)"

		op := &text.DrawOptions{}
		op.GeoM.Translate(screenWidth/2, screenHeight-smallFontSize/2)
		op.ColorScale.ScaleWithColor(color.White)
		op.LineSpacing = smallFontSize
		op.PrimaryAlign = text.AlignCenter
		op.SecondaryAlign = text.AlignEnd
		text.Draw(screen, msg, &text.GoTextFace{
			Source: arcadeFaceSource,
			Size:   smallFontSize,
		}, op)
	}

	op = &text.DrawOptions{}
	op.GeoM.Translate(screenWidth, 0)
	op.ColorScale.ScaleWithColor(color.White)
	op.LineSpacing = fontSize
	op.PrimaryAlign = text.AlignEnd
	text.Draw(screen, fmt.Sprintf("%04d", g.score()), &text.GoTextFace{
		Source: arcadeFaceSource,
		Size:   fontSize,
	}, op)

	ebitenutil.DebugPrint(screen, fmt.Sprintf("TPS: %0.2f", ebiten.ActualTPS()))
}

func (g *Game) pipeAt(tileX int) (tileY int, ok bool) {
	if (tileX - pipeStartOffsetX) <= 0 {
		return 0, false
	}
	if floorMod(tileX-pipeStartOffsetX, pipeIntervalX) != 0 {
		return 0, false
	}
	idx := floorDiv(tileX-pipeStartOffsetX, pipeIntervalX)
	return g.pipeTileYs[idx%len(g.pipeTileYs)], true
}

func (g *Game) score() int {
	x := floorDiv(g.x16, 16) / tileSize
	if (x - pipeStartOffsetX) <= 0 {
		return 0
	}
	return floorDiv(x-pipeStartOffsetX, pipeIntervalX)
}

func (g *Game) hit() bool {
	if g.mode != ModeGame {
		return false
	}
	const (
		endWidth  = 30
		endHeight = 60
	)
	w, h := endImage.Bounds().Dx(), endImage.Bounds().Dy()
	x0 := floorDiv(g.x16, 16) + (w-endWidth)/2
	y0 := floorDiv(g.y16, 16) + (h-endHeight)/2
	x1 := x0 + endWidth
	y1 := y0 + endHeight
	if y0 < -tileSize*4 {
		return true
	}
	if y1 >= screenHeight-tileSize {
		return true
	}
	xMin := floorDiv(x0-pipeWidth, tileSize)
	xMax := floorDiv(x0+endWidth, tileSize)
	for x := xMin; x <= xMax; x++ {
		y, ok := g.pipeAt(x)
		if !ok {
			continue
		}
		if x0 >= x*tileSize+pipeWidth {
			continue
		}
		if x1 < x*tileSize {
			continue
		}
		if y0 < y*tileSize {
			return true
		}
		if y1 >= (y+pipeGapY)*tileSize {
			return true
		}
	}
	return false
}

func (g *Game) drawTiles(screen *ebiten.Image) {
	const (
		nx           = screenWidth / tileSize
		ny           = screenHeight / tileSize
		pipeTileSrcX = 128
		pipeTileSrcY = 192
	)

	op := &ebiten.DrawImageOptions{}
	for i := -2; i < nx+1; i++ {
		// ground
		op.GeoM.Reset()
		op.GeoM.Translate(float64(i*tileSize-floorMod(g.cameraX, tileSize)),
			float64((ny-1)*tileSize-floorMod(g.cameraY, tileSize)))
		screen.DrawImage(tilesImage.SubImage(image.Rect(0, 0, tileSize, tileSize)).(*ebiten.Image), op)

		// pipe
		if tileY, ok := g.pipeAt(floorDiv(g.cameraX, tileSize) + i); ok {
			for j := 0; j < tileY; j++ {
				op.GeoM.Reset()
				op.GeoM.Scale(1, -1)
				op.GeoM.Translate(float64(i*tileSize-floorMod(g.cameraX, tileSize)),
					float64(j*tileSize-floorMod(g.cameraY, tileSize)))
				op.GeoM.Translate(0, tileSize)
				var r image.Rectangle
				if j == tileY-1 {
					r = image.Rect(pipeTileSrcX, pipeTileSrcY, pipeTileSrcX+tileSize*2, pipeTileSrcY+tileSize)
				} else {
					r = image.Rect(pipeTileSrcX, pipeTileSrcY+tileSize, pipeTileSrcX+tileSize*2, pipeTileSrcY+tileSize*2)
				}
				screen.DrawImage(tilesImage.SubImage(r).(*ebiten.Image), op)
			}
			for j := tileY + pipeGapY; j < screenHeight/tileSize-1; j++ {
				op.GeoM.Reset()
				op.GeoM.Translate(float64(i*tileSize-floorMod(g.cameraX, tileSize)),
					float64(j*tileSize-floorMod(g.cameraY, tileSize)))
				var r image.Rectangle
				if j == tileY+pipeGapY {
					r = image.Rect(pipeTileSrcX, pipeTileSrcY, pipeTileSrcX+pipeWidth, pipeTileSrcY+tileSize)
				} else {
					r = image.Rect(pipeTileSrcX, pipeTileSrcY+tileSize, pipeTileSrcX+pipeWidth, pipeTileSrcY+tileSize+tileSize)
				}
				screen.DrawImage(tilesImage.SubImage(r).(*ebiten.Image), op)
			}
		}
	}
}

func (g *Game) drawEnd(screen *ebiten.Image) {
	op := &ebiten.DrawImageOptions{}
	w, h := endImage.Bounds().Dx(), endImage.Bounds().Dy()
	op.GeoM.Translate(-float64(w)/2.0, -float64(h)/2.0)
	op.GeoM.Rotate(float64(g.vy16) / 96.0 * math.Pi / 6) //キャラクターの回転
	op.GeoM.Translate(float64(w)/2.0, float64(h)/2.0)
	op.GeoM.Translate(float64(g.x16/16.0)-float64(g.cameraX), float64(g.y16/16.0)-float64(g.cameraY))
	op.Filter = ebiten.FilterLinear
	screen.DrawImage(endImage, op)
}

type GameWithCRTEffect struct {
	ebiten.Game

	crtShader *ebiten.Shader
}

func (g *GameWithCRTEffect) DrawFinalScreen(screen ebiten.FinalScreen, offscreen *ebiten.Image, geoM ebiten.GeoM) {
	if g.crtShader == nil {
		s, err := ebiten.NewShader(crtGo)
		if err != nil {
			panic(fmt.Sprintf("flappy: failed to compiled the CRT shader: %v", err))
		}
		g.crtShader = s
	}

	os := offscreen.Bounds().Size()

	op := &ebiten.DrawRectShaderOptions{}
	op.Images[0] = offscreen
	op.GeoM = geoM
	screen.DrawRectShader(os.X, os.Y, g.crtShader, op)
}

func main() {
	flag.Parse()
	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("Flappy Voice(仮)")
	if err := ebiten.RunGame(NewGame(*flagCRT)); err != nil {
		panic(err)
	}
}
