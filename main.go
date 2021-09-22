package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/msawahara/ipkvm/usbgadget"
	"github.com/notedit/gst"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"golang.org/x/net/websocket"
	"gopkg.in/yaml.v2"
)

type ConfigCommand struct {
	Name    string `yaml:"name"`
	Command string `yaml:"command"`
}

type Config struct {
	ListenAddress string   `yaml:"listenAddress"`
	IceServers    []string `yaml:"iceServers"`
	Default       struct {
		RemoteVideo   bool `yaml:"remoteVideo"`
		RelativeMouse bool `yaml:"relativeMouse"`
		AbsoluteMouse bool `yaml:"absoluteMouse"`
		TouchScreen   bool `yaml:"touchScreen"`
		Keyboard      bool `yaml:"keyboard"`
		Gamepad       bool `yaml:"gamepad"`
	} `yaml:"default"`
	Commands []ConfigCommand `yaml:"commands"`
}

type KeyboardEvent struct {
	Code     []int `json:"code"`
	AltKey   bool  `json:"altKey"`
	CtrlKey  bool  `json:"ctrlKey"`
	MetaKey  bool  `json:"metaKey"`
	ShiftKey bool  `json:"shiftKey"`
}

type MouseEvent struct {
	Buttons int `json:"buttons"`
	Pos     struct {
		X int `json:"x"`
		Y int `json:"y"`
	} `json:"pos"`
}

type MouseAbsEvent MouseEvent
type TouchEvent MouseEvent

type GamepadEvent struct {
	Buttons []bool    `json:"buttons"`
	Axes    []float64 `json:"axes"`
}

type RunCommandRequest struct {
	Index int `json:"index"`
}

type VideoRequest struct {
	Enable        bool `json:"enable"`
	Width         int  `json:"width"`
	Height        int  `json:"height"`
	Framerate     int  `json:"framerate"`
	TargetBitrate int  `json:"targetBitrate"`
}

type InitRequest struct {
	RemoteVideo VideoRequest `json:"remoteVideo"`
	Mouse       bool         `json:"mouse"`
	MouseAbs    bool         `json:"mouseAbs"`
	TouchScreen bool         `json:"touchScreen"`
	Keyboard    bool         `json:"keyboard"`
	Gamepad     bool         `json:"gamepad"`
}

type WSRequest struct {
	MessageType string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
}

type TrackContext struct {
	Track *webrtc.TrackLocalStaticSample
	Stop  chan struct{}
}

type KVMContext struct {
	Usb         *usbgadget.USBGadget
	Mouse       *usbgadget.USBGadgetMouse
	MouseAbs    *usbgadget.USBGadgetMouseAbsolute
	TouchScreen *usbgadget.USBGadgetTouchScreen
	Keyboard    *usbgadget.USBGadgetKeyboard
	Gamepad     *usbgadget.USBGadgetGamePad
	Echo        echo.Context
	WS          *websocket.Conn
	PC          *webrtc.PeerConnection
	AudioTrack  *TrackContext
	VideoTrack  *TrackContext
}

var config Config

func sendOffer(ws *websocket.Conn, offer webrtc.SessionDescription) error {
	offerJson, err := json.Marshal(offer)
	if err != nil {
		return err
	}

	req := WSRequest{
		MessageType: "offer",
		Payload:     offerJson,
	}
	err = websocket.JSON.Send(ws, req)

	return err
}

func writeSamplesFromGst(c *TrackContext, name, pipelineStr string, logger echo.Logger) {
	pipeline, err := gst.ParseLaunch(fmt.Sprintf("%s ! appsink name=%s", pipelineStr, name))
	if err != nil {
		logger.Error(err)
		return
	}

	element := pipeline.GetByName(name)
	pipeline.SetState(gst.StatePlaying)

	defer func() {
		pipeline.SetState(gst.StateNull)
		logger.Infof("stream closed (name: %s)", name)
	}()

	count := 0
	for {
		sample, err := element.PullSample()
		if err != nil {
			if element.IsEOS() {
				break
			} else {
				logger.Error(err)
			}
		}

		if count == 0 {
			logger.Infof("write first sample to stream (name: %s)", name)
		}
		logger.Debugf("write sample (name: %s, count: %d, duration: %d)", name, count, sample.Duration)

		err = c.Track.WriteSample(media.Sample{Data: sample.Data, Duration: time.Duration(sample.Duration)})
		if err != nil {
			logger.Error(err)
		}

		count++

		select {
		case <-c.Stop:
			return
		default:
			// NOP
		}
	}
}

func newTrackGst(name, mimeType, pipelineStr string, logger echo.Logger) *TrackContext {
	track := new(TrackContext)
	track.Track, _ = webrtc.NewTrackLocalStaticSample(webrtc.RTPCodecCapability{MimeType: mimeType}, name, name)
	track.Stop = make(chan struct{})

	go writeSamplesFromGst(track, name, pipelineStr, logger)

	return track
}

func OnICEConnectionClose(c *KVMContext) {
	if c.AudioTrack != nil {
		close(c.AudioTrack.Stop)
	}

	if c.VideoTrack != nil {
		close(c.VideoTrack.Stop)
	}
}

func initWebRTC(c *KVMContext, v VideoRequest) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: config.IceServers,
			},
		},
	}

	c.PC, _ = webrtc.NewPeerConnection(config)

	c.PC.OnICEConnectionStateChange(func(s webrtc.ICEConnectionState) {
		c.Echo.Logger().Infof("OnIceConnectionStateChange: %s", s.String())

		if s == webrtc.ICEConnectionStateClosed {
			OnICEConnectionClose(c)
		}
	})

	c.PC.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			c.Echo.Logger().Debugf("OnIceCandidate: %s", candidate.String())
			cJson, _ := json.Marshal(candidate.ToJSON())
			req := WSRequest{
				MessageType: "addIceCandidate",
				Payload:     cJson,
			}
			websocket.JSON.Send(c.WS, req)
		} else {
			c.Echo.Logger().Debug("OnIceCandidate: compilete.")
		}
	})

	c.AudioTrack = newTrackGst(
		"audio",
		"audio/opus",
		"alsasrc device=hw:1 ! audio/x-raw,format=S16LE,rate=48000,channels=2 ! audioconvert ! opusenc",
		c.Echo.Logger(),
	)
	c.PC.AddTrack(c.AudioTrack.Track)

	blockSize := 16
	widthPad := (blockSize - (v.Width % blockSize)) % blockSize
	heightPad := (blockSize - (v.Height % blockSize)) % blockSize

	c.VideoTrack = newTrackGst(
		"video",
		"video/h264",
		fmt.Sprintf(
			"v4l2src device=/dev/video0"+
				" ! image/jpeg,width=%d,height=%d,framerate=%d/1"+
				" ! jpegdec"+
				" ! videobalance brightness=0.053887 contrast=0.858824 saturation=0.875"+
				" ! videobox right=%d bottom=%d"+
				" ! videoconvert"+
				" ! omxh264enc target-bitrate=%d control-rate=1",
			v.Width,
			v.Height,
			v.Framerate,
			-widthPad,
			-heightPad,
			v.TargetBitrate*1000,
		),
		c.Echo.Logger(),
	)
	c.PC.AddTrack(c.VideoTrack.Track)

	offer, _ := c.PC.CreateOffer(nil)
	c.PC.SetLocalDescription(offer)
	sendOffer(c.WS, offer)
}

func runCommand(c *KVMContext, wsReq WSRequest) {
	var r RunCommandRequest
	json.Unmarshal(wsReq.Payload, &r)

	if r.Index < 0 || r.Index >= len(config.Commands) {
		c.Echo.Logger().Error("invalid command index: " + strconv.Itoa(r.Index))
		return
	}

	c.Echo.Logger().Info("run command: " + config.Commands[r.Index].Command)
	err := exec.Command("sh", "-c", config.Commands[r.Index].Command).Run()
	if err != nil {
		c.Echo.Logger().Error(err)
	}
}

func onInitRequest(c *KVMContext, wsReq WSRequest) {
	var r InitRequest
	json.Unmarshal(wsReq.Payload, &r)

	if r.RemoteVideo.Enable {
		initWebRTC(c, r.RemoteVideo)
	}

	enableUsb := r.Mouse || r.MouseAbs || r.TouchScreen || r.Keyboard || r.Gamepad
	if enableUsb {
		c.Usb = usbgadget.NewUSBGadget("g0")
		if r.Mouse {
			c.Mouse = c.Usb.AddMouse("mouse")
		}
		if r.MouseAbs {
			c.MouseAbs = c.Usb.AddMouseAbsolute("mouseAbs")
		}
		if r.TouchScreen {
			c.TouchScreen = c.Usb.AddTouchScreen("touchScreen")
		}
		if r.Keyboard {
			c.Keyboard = c.Usb.AddKeyboard("keyboard")
		}
		if r.Gamepad {
			c.Gamepad = c.Usb.AddGamePad("gamepad")
		}
		c.Usb.Start()
	}
}

func onMouseEvent(c *KVMContext, wsReq WSRequest) {
	var e MouseEvent
	json.Unmarshal(wsReq.Payload, &e)

	if c.Mouse != nil {
		c.Mouse.Send(e.Buttons, e.Pos.X, e.Pos.Y)
	}
}

func onMouseAbsEvent(c *KVMContext, wsReq WSRequest) {
	var e MouseAbsEvent
	json.Unmarshal(wsReq.Payload, &e)

	if c.MouseAbs != nil {
		c.MouseAbs.Send(e.Buttons, e.Pos.X, e.Pos.Y)
	}
}

func onTouchEvent(c *KVMContext, wsReq WSRequest) {
	var e TouchEvent
	json.Unmarshal(wsReq.Payload, &e)

	if c.TouchScreen != nil {
		c.TouchScreen.Send(e.Buttons, e.Pos.X, e.Pos.Y)
	}
}

func onKeyboardEvent(c *KVMContext, wsReq WSRequest) {
	var e KeyboardEvent
	json.Unmarshal(wsReq.Payload, &e)

	if c.Keyboard != nil {
		c.Keyboard.Send(e.Code, e.AltKey, e.CtrlKey, e.MetaKey, e.ShiftKey)
	}
}

func onGamepadEvent(c *KVMContext, wsReq WSRequest) {
	var e GamepadEvent
	json.Unmarshal(wsReq.Payload, &e)

	if c.Gamepad != nil {
		c.Gamepad.Send(e.Buttons, e.Axes)
	}
}

func onReceiveAnswer(c *KVMContext, wsReq WSRequest) {
	var sdp webrtc.SessionDescription
	json.Unmarshal(wsReq.Payload, &sdp)

	if c.PC != nil {
		c.PC.SetRemoteDescription(sdp)
	}
}

func addIceCandidate(c *KVMContext, wsReq WSRequest) {
	var candidate webrtc.ICECandidateInit
	json.Unmarshal(wsReq.Payload, &candidate)

	if c.PC != nil {
		c.PC.AddICECandidate(candidate)
	}
}

func onWSClose(c *KVMContext) {
	c.WS.Close()

	if c.PC != nil {
		c.PC.Close()
	}

	if c.Usb != nil {
		c.Mouse = nil
		c.MouseAbs = nil
		c.TouchScreen = nil
		c.Keyboard = nil
		c.Gamepad = nil

		c.Usb.Stop()
	}
}

func wsHandler(ws *websocket.Conn, e echo.Context) {
	c := new(KVMContext)
	c.WS = ws
	c.Echo = e

	defer onWSClose(c)

	for {
		var req WSRequest
		err := websocket.JSON.Receive(c.WS, &req)
		if err != nil {
			if err.Error() == "EOF" {
				c.Echo.Logger().Info("WebSocket closed")
			} else {
				c.Echo.Logger().Error(err)
			}
			break
		}

		switch req.MessageType {
		case "init":
			onInitRequest(c, req)
		case "mouseEvent":
			onMouseEvent(c, req)
		case "mouseAbsEvent":
			onMouseAbsEvent(c, req)
		case "touchEvent":
			onTouchEvent(c, req)
		case "keyEvent":
			onKeyboardEvent(c, req)
		case "gamepadEvent":
			onGamepadEvent(c, req)
		case "answer":
			onReceiveAnswer(c, req)
		case "addIceCandidate":
			addIceCandidate(c, req)
		case "runCommand":
			runCommand(c, req)
		case "keepAlive":
			// NOP
		default:
			c.Echo.Logger().Error("invalid message type: " + req.MessageType)
		}
	}
}

func wsEndpoint(c echo.Context) error {
	websocket.Handler(func(ws *websocket.Conn) { wsHandler(ws, c) }).ServeHTTP(c.Response(), c.Request())
	return nil
}

func loadConfig(filename string) error {
	buf, err := ioutil.ReadFile(filename)
	if err != nil {
		return err
	}

	err = yaml.Unmarshal(buf, &config)
	return err
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func main() {
	err := loadConfig("config.yaml")
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	t := &Template{
		templates: template.Must(template.ParseGlob("templates/*.html")),
	}

	e := echo.New()
	e.Renderer = t
	e.Logger.SetLevel(log.INFO)
	e.Use(middleware.Logger())
	e.GET("/api/ws", wsEndpoint)
	e.GET("/", func(c echo.Context) error {
		return c.Render(http.StatusOK, "kvm", config)
	})
	e.Logger.Fatal(e.Start(config.ListenAddress))
}
