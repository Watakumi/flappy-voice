### setup

```
brew install portaudio
```

### 音声の変換

```
brew install ffmpeg
```

```
ffmpeg -i path -acodec pcm_s16le -ac 1 -ar 48000 path.wav
```
