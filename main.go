package main

import (
	"encoding/json"
	"fmt"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/msawahara/ipkvm/usbgadget"
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

type InitRequest struct {
	Mouse    bool `json:"mouse"`
	Keyboard bool `json:"keyboard"`
}

type WSRequest struct {
	MessageType string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
}

func onInitRequest(wsReq WSRequest) {
	var r InitRequest
	json.Unmarshal(wsReq.Payload, &r)

	usbg = usbgadget.NewUSBGadget("g0")
	if r.Mouse {
		mouse = usbg.AddMouse("mouse")
	}
	if r.Keyboard {
		keyboard = usbg.AddKeyboard("keyboard")
	}
	usbg.Start()
}

func onMouseEvent(wsReq WSRequest) {
	var e MouseEvent
	json.Unmarshal(wsReq.Payload, &e)
	if mouse != nil {
		mouse.Send(e.Buttons, e.Pos.X, e.Pos.Y)
	}
}

func onKeyboardEvent(wsReq WSRequest) {
	var e KeyboardEvent
	json.Unmarshal(wsReq.Payload, &e)

	keyboard.Send(e.Code, e.AltKey, e.CtrlKey, e.MetaKey, e.ShiftKey)
	fmt.Println(e)
}

func onClose() {
	if usbg != nil {
		usbg.Stop()
	}
	usbg = nil
	mouse = nil
	keyboard = nil
}

func wsHandler(ws *websocket.Conn, c echo.Context) {
	defer ws.Close()
	defer onClose()
	for {
		var req WSRequest
		err := websocket.JSON.Receive(ws, &req)
		if err != nil {
			c.Logger().Error(err)
			break
		}

		switch req.MessageType {
		case "init":
			onInitRequest(req)
		case "mouseEvent":
			onMouseEvent(req)
		case "keyEvent":
			onKeyboardEvent(req)
		case "keepAlive":
			// NOP
		default:
			c.Logger().Error("Unknown type: " + req.MessageType)
		}
	}
}

func wsEndpoint(c echo.Context) error {
	websocket.Handler(func(ws *websocket.Conn) { wsHandler(ws, c) }).ServeHTTP(c.Response(), c.Request())
	return nil
}

var (
	usbg     *usbgadget.USBGadget
	mouse    *usbgadget.USBGadgetMouse
	keyboard *usbgadget.USBGadgetKeyboard
)

func main() {
	e := echo.New()
	e.Use(middleware.Logger())
	e.GET("/api/ws", wsEndpoint)
	e.Logger.Fatal(e.Start(":1323"))
}
