package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"github.com/msawahara/ipkvm/usbgadget"
	"github.com/notedit/gst"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"golang.org/x/net/websocket"
)

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

type InitRequest struct {
	RemoteVideo struct {
		Enable    bool `json:"enable"`
		Width     int  `json:"width"`
		Height    int  `json:"height"`
		Framerate int  `json:"framerate"`
	} `json:"remoteVideo"`
	Mouse       bool `json:"mouse"`
	MouseAbs    bool `json:"mouseAbs"`
	TouchScreen bool `json:"touchScreen"`
	Keyboard    bool `json:"keyboard"`
	Gamepad     bool `json:"gamepad"`
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

func initWebRTC(c *KVMContext, width, height, framerate int) {
	config := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
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

	c.VideoTrack = newTrackGst(
		"video",
		"video/h264",
		fmt.Sprintf("v4l2src device=/dev/video0 ! image/jpeg,width=%d,height=%d,framerate=%d/1 ! jpegdec ! videobalance brightness=0.053887 contrast=0.858824 saturation=0.875 ! videoconvert ! omxh264enc target-bitrate=3000000 control-rate=1", width, height, framerate),
		c.Echo.Logger(),
	)
	c.PC.AddTrack(c.VideoTrack.Track)

	offer, _ := c.PC.CreateOffer(nil)
	c.PC.SetLocalDescription(offer)
	sendOffer(c.WS, offer)
}

func onInitRequest(c *KVMContext, wsReq WSRequest) {
	var r InitRequest
	json.Unmarshal(wsReq.Payload, &r)

	if r.RemoteVideo.Enable {
		initWebRTC(c, r.RemoteVideo.Width, r.RemoteVideo.Height, r.RemoteVideo.Framerate)
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

func main() {
	e := echo.New()
	e.Logger.SetLevel(log.INFO)
	e.Use(middleware.Logger())
	e.GET("/api/ws", wsEndpoint)
	e.File("/", "kvm.html")
	e.Logger.Fatal(e.Start(":1323"))
}
