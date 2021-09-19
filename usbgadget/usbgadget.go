package usbgadget

import (
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

/* device information */
const (
	USB_VENDOR_ID         int    = 0x1d6b // The Linux Foundation
	USB_PRODUCT_ID        int    = 0x0104 // Multifunction Composite Gadget
	USB_VERSION           int    = 0x0200 // USB 2.0
	USB_DEVICE_VERSION    int    = 0x0100 // v.1.0.0
	USB_DESC_LANG_ID      int    = 0x0409 // en-US
	USB_DESC_SERIAL       string = "00000000"
	USB_DESC_MANUFACTURER string = "The Linux Foundation"
	USB_DESC_PRODUCT_NAME string = "Generic USB Device"
)

/* USB subclass */
const (
	USB_SUBCLASS_NO_SUBCLASS    int = 0
	USB_SUBCLASS_BOOT_INTERFACE int = 1
)

/* USB protocol */
const (
	USB_PROTOCOL_NONE     int = 0
	USB_PROTOCOL_KEYBOARD int = 1
	USB_PROTOCOL_MOUSE    int = 2
)

type USBGadgetDevice struct {
	ConfigDir string
	Device    string
}

type USBGadgetMouse struct {
	X      int
	Y      int
	Device USBGadgetDevice
}

type USBGadgetMouseAbsolute struct {
	Device USBGadgetDevice
}

type USBGadgetTouchScreen struct {
	Device USBGadgetDevice
}

type USBGadgetKeyboard struct {
	Device USBGadgetDevice
}

type USBGadgetGamePad struct {
	Device USBGadgetDevice
}

type USBGadgetFunction struct {
	Type             string
	Protocol         int
	SubClass         int
	NoOutEndpoint    bool
	ReportLength     int
	ReportDescriptor []byte
}

type USBGadgetStringDescriptor struct {
	SerialNumber string
	Manufacturer string
	Product      string
}

type USBGadget struct {
	Name          string
	MaxPacketSize int
	IdVendor      int
	IdProduct     int
	UsbVersion    int
	DeviceVesion  int
	Strings       map[int]*USBGadgetStringDescriptor
	Functions     map[string]*USBGadgetFunction
}

var configFsDir string = "/sys/kernel/config"

func getGadgetDir(gadgetName string) string {
	return configFsDir + "/usb_gadget/" + gadgetName
}

func getConfigDir(gadgetName string) string {
	return getGadgetDir(gadgetName) + "/configs/c.1"
}

func min(a, b int) int {
	if a > b {
		return b
	}
	return a
}

func (d *USBGadgetDevice) Get() (string, error) {
	if len(d.Device) != 0 {
		return d.Device, nil
	}

	data, _ := ioutil.ReadFile(d.ConfigDir + "/dev")
	ids := strings.Split(strings.TrimRight(string(data), "\n"), ":")
	major, _ := strconv.Atoi(ids[0])
	minor, _ := strconv.Atoi(ids[1])

	files, _ := ioutil.ReadDir("/dev")
	for _, file := range files {
		if (file.Mode() & os.ModeCharDevice) == 0 {
			continue
		}
		name := "/dev/" + file.Name()
		stat := syscall.Stat_t{}
		syscall.Stat(name, &stat)
		majorDev := int64(stat.Rdev / 256)
		minorDev := int64(stat.Rdev % 256)
		if major == int(majorDev) && minor == int(minorDev) {
			d.Device = name
			break
		}
	}

	if len(d.Device) == 0 {
		return "", errors.New("device not found")
	}

	return d.Device, nil
}

func (k *USBGadgetKeyboard) Send(code []int, altKey, ctrlKey, metaKey, shiftKey bool) error {
	dev, err := k.Device.Get()
	if err != nil {
		return err
	}

	modifier := byte(0)

	if ctrlKey {
		modifier |= 1
	}
	if shiftKey {
		modifier |= 2
	}
	if altKey {
		modifier |= 4
	}
	if metaKey {
		modifier |= 8
	}

	report_keys := code[:min(6, len(code))]

	report := make([]byte, 8)
	report[0] = modifier // Modifier
	report[1] = 0        // Reserved
	for i, c := range report_keys {
		report[2+i] = byte(c) // Keycodes
	}

	err = ioutil.WriteFile(dev, report, 0600)

	return err
}

func (m *USBGadgetMouse) Send(buttons, x, y int) error {
	dev, err := m.Device.Get()
	if err != nil {
		return err
	}

	dx := int8(math.Max(math.Min(float64(x-m.X), 127), -127))
	dy := int8(math.Max(math.Min(float64(y-m.Y), 127), -127))

	m.X = x
	m.Y = y

	report := make([]byte, 3)
	report[0] = byte(buttons & 0x07)
	report[1] = byte(dx)
	report[2] = byte(dy)

	err = ioutil.WriteFile(dev, report, 0600)

	return err
}

func (m *USBGadgetMouseAbsolute) Send(buttons, x, y int) error {
	dev, err := m.Device.Get()
	if err != nil {
		return err
	}

	report := make([]byte, 6)
	report[0] = byte(buttons & 0x07)
	report[1] = 0 // padding
	report[2] = byte(x & 0xff)
	report[3] = byte((x >> 8) & 0xff)
	report[4] = byte(y & 0xff)
	report[5] = byte((y >> 8) & 0xff)

	err = ioutil.WriteFile(dev, report, 0600)

	return err
}

func (m *USBGadgetTouchScreen) Send(buttons, x, y int) error {
	dev, err := m.Device.Get()
	if err != nil {
		return err
	}

	report := make([]byte, 7)
	report[0] = 1 // contact count
	report[1] = 0 // contact identifier
	report[2] = byte(buttons&1) | 0x02
	report[3] = byte(x & 0xff)
	report[4] = byte((x >> 8) & 0xff)
	report[5] = byte(y & 0xff)
	report[6] = byte((y >> 8) & 0xff)

	err = ioutil.WriteFile(dev, report, 0600)

	return err
}

func (m *USBGadgetGamePad) Send(buttons []bool, axes []float64) error {
	dev, err := m.Device.Get()
	if err != nil {
		return err
	}

	// hat switch mapping (Up, Down, Left, Right)
	hatSwitchMap := []int{12, 13, 14, 15}

	// buttons mapping
	buttonMap := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 16}

	// make report buffer
	report := make([]byte, 7)

	// hat switch
	var hatSwitch byte
	for i, n := range hatSwitchMap {
		if n >= len(buttons) {
			continue
		}
		if buttons[n] == true {
			hatSwitch |= 1 << i
		}
	}

	var reportHat byte
	switch hatSwitch {
	case 0x01: // ___U
		reportHat = 0x00
	case 0x09: // R__U
		reportHat = 0x01
	case 0x08: // R___
		reportHat = 0x02
	case 0x0a: // R_D_
		reportHat = 0x03
	case 0x02: // __D_
		reportHat = 0x04
	case 0x06: // _LD_
		reportHat = 0x05
	case 0x04: // _L__
		reportHat = 0x06
	case 0x05: // _L_U
		reportHat = 0x07
	default:
		reportHat = 0x0f
	}

	report[0] = reportHat

	// buttons
	for i, n := range buttonMap {
		if n >= len(buttons) {
			continue
		}
		if buttons[n] == true {
			report[i/8+1] |= 1 << (i % 8)
		}
	}

	// axes
	reportAxes := axes[:min(4, len(axes))]
	for i, v := range reportAxes {
		report[3+i] = byte((v + 1) / 2 * 255)
	}

	err = ioutil.WriteFile(dev, report, 0600)

	return err
}

func (g USBGadget) AddMouse(name string) *USBGadgetMouse {
	f := new(USBGadgetFunction)
	f.Type = "hid"
	f.Protocol = USB_PROTOCOL_MOUSE
	f.SubClass = USB_SUBCLASS_BOOT_INTERFACE
	f.NoOutEndpoint = true
	f.ReportLength = 3
	f.ReportDescriptor = []byte{
		0x05, 0x01, // [G] 05: Usage Page      (bSize = 1), 01: Generic Desktop
		0x09, 0x02, // [L] 09: Usage           (bSize = 1), 02: Mouse (in Generic Desktop Page)
		0xa1, 0x01, // [M] a1: Collection      (bSize = 1), 01: Application

		0x09, 0x01, // [L] 09: Usage           (bSize = 1), 01: Pointer (in Generic Desktop Page)
		0xa1, 0x00, // [M] a1: Collection      (bSize = 1), 00: Physical

		// Input: buttons, 1 byte (1 bit/field * 3 fields + padding)
		0x95, 0x03, // [G] 95: Report Count    (bSize = 1), 03: 3 fields
		0x75, 0x01, // [G] 75: Report Size     (bSize = 1), 01: 1 bits/field
		0x05, 0x09, // [G] 05: Usage Page      (bSize = 1), 09: Button
		0x19, 0x01, // [L] 19: Usage Minimum   (bSize = 1), 01: Button 1, Selector (in Keyboard/Keypad Page)
		0x29, 0x03, // [L] 29: Usage Maximum   (bSize = 1), 03: Button 3, Selector (in Keyboard/Keypad Page)
		0x15, 0x00, // [G] 15: Logical Minimum (bSize = 1), 00: 0
		0x25, 0x01, // [G] 25: Logical Maximum (bSize = 1), 01: 1
		0x81, 0x02, // [M] 81: Input           (bSize = 1), 02: Variable, Data, Absolute
		0x95, 0x01, // [G] 95: Report Count    (bSize = 1), 01: 1 fields
		0x75, 0x05, // [G] 75: Report Size     (bSize = 1), 05: 5 bits/field
		0x81, 0x01, // [M] 81: Input           (bSize = 1), 03: Constant (for padding)

		// Input: X, Y, 2 byte (8 bits/field * 2 fields)
		0x75, 0x08, // [G] 75: Report Size     (bSize = 1), 08: 8 bits/field
		0x95, 0x02, // [G] 95: Report Count    (bSize = 1), 02: 2 fields
		0x05, 0x01, // [G] 05: Usage Page      (bSize = 1), 01: Generic Desktop
		0x09, 0x30, // [L] 09: Usage           (bSize = 1), 30: X, Dynamic Value (in Generic Desktop Page)
		0x09, 0x31, // [L] 09: Usage           (bSize = 1), 31: Y, Dynamic Value (in Generic Desktop Page)
		0x15, 0x81, // [G] 15: Logical Minimum (bSize = 1), 81: -127
		0x25, 0x7f, // [G] 25: Logical Maximum (bSize = 1), 7f: 127
		0x81, 0x06, // [M] 81: Input           (bSize = 1), 06: Variable, Data, Relative

		0xc0, //       [M] c0: End Collection
		0xc0, //       [M] c0: End Collection
	}
	g.AddFunction(name, f)

	m := new(USBGadgetMouse)
	m.Device.ConfigDir = getConfigDir(g.Name) + fmt.Sprintf("/%s.%s", f.Type, name)

	return m
}

func (g USBGadget) AddMouseAbsolute(name string) *USBGadgetMouseAbsolute {
	f := new(USBGadgetFunction)
	f.Type = "hid"
	f.Protocol = USB_PROTOCOL_NONE
	f.SubClass = USB_SUBCLASS_NO_SUBCLASS
	f.NoOutEndpoint = true
	f.ReportLength = 6
	f.ReportDescriptor = []byte{
		0x05, 0x01, // [G] 05: Usage Page      (bSize = 1), 01: Generic Desktop
		0x09, 0x02, // [L] 09: Usage           (bSize = 1), 02: Mouse (in Generic Desktop Page)
		0xa1, 0x01, // [M] a1: Collection      (bSize = 1), 01: Application

		0x09, 0x01, // [L] 09: Usage           (bSize = 1), 01: Pointer (in Generic Desktop Page)
		0xa1, 0x00, // [M] a1: Collection      (bSize = 1), 00: Physical

		// Input: buttons, 2 byte (1 bit/field * 3 fields + padding)
		0x05, 0x09, // [G] 05: Usage Page      (bSize = 1), 09: Button
		0x19, 0x01, // [L] 19: Usage Minimum   (bSize = 1), 01: Button 1, Selector (in Keyboard/Keypad Page)
		0x29, 0x03, // [L] 29: Usage Maximum   (bSize = 1), 03: Button 3, Selector (in Keyboard/Keypad Page)
		0x15, 0x00, // [G] 15: Logical Minimum (bSize = 1), 00: 0
		0x25, 0x01, // [G] 25: Logical Maximum (bSize = 1), 01: 1
		0x75, 0x01, // [G] 75: Report Size     (bSize = 1), 01: 1 bits/field
		0x95, 0x03, // [G] 95: Report Count    (bSize = 1), 03: 3 fields
		0x81, 0x02, // [M] 81: Input           (bSize = 1), 02: Variable, Data, Absolute
		0x75, 0x0d, // [G] 75: Report Size     (bSize = 1), 0d: 13 bits/field
		0x95, 0x01, // [G] 95: Report Count    (bSize = 1), 01: 1 fields
		0x81, 0x01, // [M] 81: Input           (bSize = 1), 03: Constant (for padding)

		// Input: X, Y, 4 byte (16 its/field * 2 fields)
		0x05, 0x01, // [G] 05: Usage Page      (bSize = 1), 01: Generic Desktop
		0x09, 0x30, // [L] 09: Usage           (bSize = 1), 30: X, Dynamic Value (in Generic Desktop Page)
		0x09, 0x31, // [L] 09: Usage           (bSize = 1), 31: Y, Dynamic Value (in Generic Desktop Page)
		0x15, 0x00, // [G] 15: Logical Minimum (bSize = 1), 00: 0
		0x26,       // [G] 26: Logical Maximum (bSize = 2),
		0xff, 0x7f, //                                      7fff: 32767
		0x75, 0x10, // [G] 75: Report Size     (bSize = 1), 10: 16 bits/field
		0x95, 0x02, // [G] 95: Report Count    (bSize = 1), 02: 2 fields
		0x81, 0x02, // [M] 81: Input           (bSize = 1), 02: Variable, Data, Absolute

		0xc0, //       [M] c0: End Collection

		0xc0, //       [M] c0: End Collection
	}
	g.AddFunction(name, f)

	mouseAbs := new(USBGadgetMouseAbsolute)
	mouseAbs.Device.ConfigDir = getConfigDir(g.Name) + fmt.Sprintf("/%s.%s", f.Type, name)

	return mouseAbs
}

func (g USBGadget) AddTouchScreen(name string) *USBGadgetTouchScreen {
	f := new(USBGadgetFunction)
	f.Type = "hid"
	f.Protocol = USB_PROTOCOL_NONE
	f.SubClass = USB_SUBCLASS_NO_SUBCLASS
	f.NoOutEndpoint = true
	f.ReportLength = 7
	f.ReportDescriptor = []byte{
		0x05, 0x0d, // [G] 05: Usage Page      (bSize = 1), 0d: Digitizers
		0x09, 0x04, // [L] 09: Usage           (bSize = 1), 04: Touch Screen (in Digitizers Page)
		0xa1, 0x01, // [M] a1: Collection      (bSize = 1), 01: Application

		0x09, 0x55, // [L] 09: Usage           (bSize = 1), 55: Count Maximum (in Digitizers Page)
		0x25, 0x01, // [G] 25: Logical Maximum (bSize = 1), 01: 1
		0xb1, 0x02, // [M] 81: Feature         (bSize = 1), 02: Variable, Data, Absolute

		// Input: contact count, 1 byte (8 bits/field * 1 field)
		0x09, 0x54, // [L] 09: Usage           (bSize = 1), 54: Contact count (in Digitizers Page)
		0x75, 0x08, // [G] 75: Report Size     (bSize = 1), 08: 8 bits/field
		0x95, 0x01, // [G] 95: Report Count    (bSize = 1), 01: 1 field
		0x81, 0x02, // [M] 81: Input           (bSize = 1), 02: Variable, Data, Absolute

		0x09, 0x22, // [L] 09: Usage           (bSize = 1), 22: Finger (in Digitizers Page)
		0xa1, 0x02, // [M] a1: Collection      (bSize = 1), 02: Logical

		// Input: contact identifier, 1 byte (8 bits/field * 1 field)
		0x09, 0x51, // [L] 09: Usage           (bSize = 1), 51: Contact Identifier (in Digitizers Page)
		0x75, 0x08, // [G] 75: Report Size     (bSize = 1), 08: 8 bits/field
		0x95, 0x01, // [G] 95: Report Count    (bSize = 1), 01: 1 field
		0x81, 0x02, // [M] 81: Input           (bSize = 1), 02: Variable, Data, Absolute

		// Input: status, 1 byte (1 bit/field * 2 fields + padding)
		0x09, 0x42, // [L] 09: Usage           (bSize = 1), 42: Tip Switch (in Digitizers Page)
		0x09, 0x32, // [L] 09: Usage           (bSize = 1), 32: In Range (in Digitizers Page)
		0x15, 0x00, // [G] 15: Logical Minimum (bSize = 1), 00: 0
		0x25, 0x01, // [G] 25: Logical Maximum (bSize = 1), 01: 1
		0x75, 0x01, // [G] 75: Report Size     (bSize = 1), 01: 1 bit/field
		0x95, 0x02, // [G] 95: Report Count    (bSize = 1), 02: 2 fields
		0x81, 0x02, // [M] 81: Input           (bSize = 1), 02: Variable, Data, Absolute
		0x95, 0x06, // [G] 95: Report Count    (bSize = 1), 06: 6 fields
		0x81, 0x01, // [M] 81: Input           (bSize = 1), 01: Constant (for padding)

		// Input: position, 4 bytes (16 bits/field * 2 fields)
		0x05, 0x01, // [G] 05: Usage Page      (bSize = 1), 01: Generic Desktop
		0x09, 0x30, // [L] 09: Usage           (bSize = 1), 30: X, Dynamic Value (in Generic Desktop Page)
		0x09, 0x31, // [L] 09: Usage           (bSize = 1), 31: Y, Dynamic Value (in Generic Desktop Page)
		0x15, 0x00, // [G] 15: Logical Minimum (bSize = 1), 00: 0
		0x26,       // [G] 26: Logical Maximum (bSize = 2),
		0xff, 0x7f, //                                      7fff: 32767
		0x55, 0x00, // [G] 55: Unit Exponent   (bSize = 1), 00: 0
		0x65, 0x00, // [G] 65: Unit            (bSize = 1), 00: None
		0x75, 0x10, // [G] 75: Report Size     (bSize = 1), 10: 16 bits/field
		0x95, 0x02, // [G] 95: Report Count    (bSize = 1), 01: 2 fields
		0x81, 0x02, // [M] 81: Input           (bSize = 1), 02: Variable, Data, Absolute

		0xc0, //       [M] c0: End Collection

		0xc0, //       [M] c0: End Collection
	}
	g.AddFunction(name, f)

	digitizer := new(USBGadgetTouchScreen)
	digitizer.Device.ConfigDir = getConfigDir(g.Name) + fmt.Sprintf("/%s.%s", f.Type, name)

	return digitizer
}

func (g USBGadget) AddKeyboard(name string) *USBGadgetKeyboard {
	f := new(USBGadgetFunction)
	f.Type = "hid"
	f.Protocol = USB_PROTOCOL_KEYBOARD
	f.SubClass = USB_SUBCLASS_BOOT_INTERFACE
	f.NoOutEndpoint = true
	f.ReportLength = 8
	f.ReportDescriptor = []byte{
		0x05, 0x01, // [G] 05: Usage Page      (bSize = 1), 01: Generic Desktop
		0x09, 0x06, // [L] 09: Usage           (bSize = 1), 06: Keyboard (in Generic Desktop Page)
		0xa1, 0x01, // [M] a1: Collection      (bSize = 1), 01: Application

		// Input: modifier keys, 1 byte (1 bit/field * 8 fields)
		0x05, 0x07, // [G] 05: Usage Page      (bSize = 1), 07: Keyboard/Keypad
		0x19, 0xe0, // [L] 19: Usage Minimum   (bSize = 1), e0: Keyboard LeftControl, Dynamic Flag (in Keyboard/Keypad Page)
		0x29, 0xe7, // [L] 29: Usage Maximum   (bSize = 1), e7: Keyboard Right GUI,   Dynamic Flag (in Keyboard/Keypad Page)
		0x15, 0x00, // [G] 15: Logical Minimum (bSize = 1), 00: 0
		0x25, 0x01, // [G] 25: Logical Maximum (bSize = 1), 01: 1
		0x75, 0x01, // [G] 75: Report Size     (bSize = 1), 01: 1 bits/field
		0x95, 0x08, // [G] 95: Report Count    (bSize = 1), 08: 8 fields
		0x81, 0x02, // [M] 81: Input           (bSize = 1), 02: Variable, Data, Absolute

		// Input: padding, 1 byte
		0x75, 0x08, // [G] 75: Report Size     (bsize = 1), 08: 8bits/field
		0x95, 0x01, // [G] 95: Report Count    (bSize = 1), 01: 1 fields
		0x81, 0x01, // [M] 81: Input           (bSize = 1), 01: Constant (for padding)

		// Input: selected keys, 6 byte (8 bits/field * 6 fields)
		0x05, 0x07, // [G] 05: Usage Page      (bSize = 1), 07: Keyboard/Keypad
		0x19, 0x00, // [L] 19: Usage Minimum   (bSize = 1), 00: Reserved (no event indicated), Selector (in Keyboard/Keypad Page)
		0x29, 0x65, // [L] 19: Usage Maximum   (bSize = 1), 65: Keyboard Application,          Selector (in Keyboard/Keypad Page)
		0x15, 0x00, // [G] 15: Logical Minimum (bSize = 1), 00: 0
		0x25, 0x65, // [G] 25: Logical Maximum (bSize = 1), 65: 101
		0x75, 0x08, // [G] 75: Report Size     (bsize = 1), 08: 8 bits/field
		0x95, 0x06, // [G] 95: Report Count    (bSize = 1), 06: 6 fields
		0x81, 0x00, // [M] 81: Input           (bSize = 1), 00: Array, Data

		0xc0, //       [M] c0: End Collection
	}
	g.AddFunction(name, f)

	k := new(USBGadgetKeyboard)
	k.Device.ConfigDir = getConfigDir(g.Name) + fmt.Sprintf("/%s.%s", f.Type, name)

	return k
}

func (g USBGadget) AddGamePad(name string) *USBGadgetGamePad {
	// not implemented
	f := new(USBGadgetFunction)
	f.Type = "hid"
	f.Protocol = USB_PROTOCOL_NONE
	f.SubClass = USB_SUBCLASS_NO_SUBCLASS
	f.NoOutEndpoint = true
	f.ReportLength = 7
	f.ReportDescriptor = []byte{
		0x05, 0x01, // [G] 05: Usage Page       (bSize = 1), 01: Generic Desktop
		0x09, 0x04, // [L] 09: Usage            (bSize = 1), 04: Game Controls (in Generic Desktop Page)
		0xa1, 0x01, // [M] a1: Collection       (bSize = 1), 01: Application

		0x09, 0x01, // [L] 09: Usage            (bSize = 1), 01: Pointer (in Generic Desktop Page)
		0xa1, 0x00, // [M] a1: Collection       (bSize = 1), 00: Physical

		// Input: hat switch, 1 byte (4bit/field * 1 fields + padding)
		0x05, 0x01, // [G] 05: Usage Page       (bSize = 1), 01: Generic Desktop
		0x09, 0x39, // [L] 09: Usage            (bSize = 1), 39: Hat switch (in Generic Desktop Page)
		0x15, 0x00, // [G] 15: Logical Minimum  (bSize = 1), 00: 0
		0x25, 0x07, // [G] 25: Logical Maximum  (bSize = 1), 01: 7
		0x35, 0x00, // [G] 35: Physical Minimum (bSize = 1), 00: 0
		0x46,       // [G] 46: Physical Maximum (bSize = 2),
		0x3b, 0x01, //                                       01: 315
		0x65, 0x14, // [G] 65: Unit             (bSize = 1), 14: Degrees
		0x75, 0x04, // [G] 75: Report Size      (bSize = 1), 04: 4 bits/field
		0x95, 0x01, // [G] 95: Report Count     (bSize = 1), 01: 1 fields
		0x81, 0x02, // [M] 81: Input            (bSize = 1), 42: Variable, Data, Absolute, Null

		0x95, 0x01, // [G] 95: Report Count     (bSize = 1), 01: 1 fields
		0x75, 0x04, // [G] 75: Report Size      (bSize = 1), 04: 4 bits/field
		0x81, 0x01, // [M] 81: Input            (bSize = 1), 03: Constant (for padding)

		// Input: buttons, 2 byte (1 bit/field * 13 fields + padding)
		0x05, 0x09, // [G] 05: Usage Page       (bSize = 1), 09: Button
		0x19, 0x01, // [L] 19: Usage Minimum    (bSize = 1), 01: Button  1 (in Button Page)
		0x29, 0x0d, // [L] 29: Usage Maximum    (bSize = 1), 0d: Button 13 (in Button Page)
		0x15, 0x00, // [G] 15: Logical Minimum  (bSize = 1), 00: 0
		0x25, 0x01, // [G] 25: Logical Maximum  (bSize = 1), 01: 1
		0x35, 0x00, // [G] 35: Physical Minimum (bSize = 1), 00: 0
		0x45, 0x01, // [G] 45: Physical Maximum (bSize = 1), 01: 1
		0x65, 0x00, // [G] 65: Unit             (bSize = 1), 00: None
		0x75, 0x01, // [G] 75: Report Size      (bSize = 1), 01:  1 bits/field
		0x95, 0x0d, // [G] 95: Report Count     (bSize = 1), 0d: 13 fields
		0x81, 0x02, // [M] 81: Input            (bSize = 1), 02: Variable, Data, Absolute

		0x95, 0x01, // [G] 95: Report Count     (bSize = 1), 01: 1 fields
		0x75, 0x03, // [G] 75: Report Size      (bSize = 1), 03: 3 bits/field
		0x81, 0x01, // [M] 81: Input            (bSize = 1), 03: Constant (for padding)

		// Input: X, Y, Z, Rz 4 byte (8 bits/field * 4 fields)
		0x05, 0x01, // [G] 05: Usage Page       (bSize = 1), 01: Generic Desktop
		0x09, 0x30, // [L] 09: Usage            (bSize = 1), 30: X, Dynamic Value (in Generic Desktop Page)
		0x09, 0x31, // [L] 09: Usage            (bSize = 1), 31: Y, Dynamic Value (in Generic Desktop Page)
		0x09, 0x32, // [L] 09: Usage            (bSize = 1), 32: Z, Dynamic Value (in Generic Desktop Page)
		0x09, 0x35, // [L] 09: Usage            (bSize = 1), 35: Rz, Dynamic Value (in Generic Desktop Page)
		0x15, 0x00, // [G] 16: Logical Minimum  (bSize = 1), 00: 0
		0x25, 0xff, // [G] 26: Logical Maximum  (bSize = 1), ff: 255
		0x35, 0x00, // [G] 35: Physical Minimum (bSize = 1), 00: 0
		0x45, 0xff, // [G] 45: Physical Maximum (bSize = 1), ff: 255
		0x75, 0x08, // [G] 75: Report Size      (bSize = 1), 08: 8 bits/field
		0x95, 0x04, // [G] 95: Report Count     (bSize = 1), 04: 4 fields
		0x81, 0x02, // [M] 81: Input            (bSize = 1), 02: Variable, Data, Absolute

		0xc0, //       [M] c0: End Collection

		0xc0, //       [M] c0: End Collection
	}
	g.AddFunction(name, f)

	gamepad := new(USBGadgetGamePad)
	gamepad.Device.ConfigDir = getConfigDir(g.Name) + fmt.Sprintf("/%s.%s", f.Type, name)

	return gamepad
}

func (g USBGadget) AddFunction(name string, f *USBGadgetFunction) {
	g.Functions[name] = f
}

func (g USBGadget) Start() {
	gadgetDir := getGadgetDir(g.Name)

	// set device infomation
	os.Mkdir(gadgetDir, 0755)
	ioutil.WriteFile(gadgetDir+"/bMaxPacketSize0", []byte(strconv.Itoa(g.MaxPacketSize)), 0644)
	ioutil.WriteFile(gadgetDir+"/idVendor", []byte(strconv.Itoa(g.IdVendor)), 0644)
	ioutil.WriteFile(gadgetDir+"/idProduct", []byte(strconv.Itoa(g.IdProduct)), 0644)
	ioutil.WriteFile(gadgetDir+"/bcdUSB", []byte(strconv.Itoa(g.UsbVersion)), 0644)
	ioutil.WriteFile(gadgetDir+"/bcdDevice", []byte(strconv.Itoa(g.DeviceVesion)), 0644)

	// create string descriptor
	for l, s := range g.Strings {
		stringsDir := gadgetDir + "/strings/" + fmt.Sprintf("0x%04x", l)

		os.Mkdir(stringsDir, 0755)
		ioutil.WriteFile(stringsDir+"/serialnumber", []byte(s.SerialNumber), 0644)
		ioutil.WriteFile(stringsDir+"/manufacturer", []byte(s.Manufacturer), 0644)
		ioutil.WriteFile(stringsDir+"/product", []byte(s.Product), 0644)
	}

	configDir := getConfigDir(g.Name)
	os.Mkdir(configDir, 0755)

	// create function directories
	for n, f := range g.Functions {
		functionDir := gadgetDir + "/functions/" + fmt.Sprintf("%s.%s", f.Type, n)

		os.Mkdir(functionDir, 0755)
		ioutil.WriteFile(functionDir+"/protocol", []byte(strconv.Itoa(f.Protocol)), 0644)
		ioutil.WriteFile(functionDir+"/subclass", []byte(strconv.Itoa(f.SubClass)), 0644)
		ioutil.WriteFile(functionDir+"/report_length", []byte(strconv.Itoa(f.ReportLength)), 0644)
		ioutil.WriteFile(functionDir+"/report_desc", f.ReportDescriptor, 0644)

		// use no_out_endpoint option if supported
		if _, err := os.Stat(functionDir + "/no_out_endpoint"); err == nil && f.NoOutEndpoint == true {
			ioutil.WriteFile(functionDir+"/no_out_endpoint", []byte("1"), 0644)
		}

		os.Symlink(functionDir, configDir+fmt.Sprintf("/%s.%s", f.Type, n))
	}

	// use first one
	files, _ := ioutil.ReadDir("/sys/class/udc")
	udc := filepath.Base(files[0].Name())

	// attach to usb device controller
	ioutil.WriteFile(gadgetDir+"/UDC", []byte(udc), 0644)
}

func (g USBGadget) Stop() {
	gadgetDir := getGadgetDir(g.Name)
	configDir := getConfigDir(g.Name)

	// detach from usb device controller
	ioutil.WriteFile(gadgetDir+"/UDC", []byte("\n"), 0644)

	// remove functions
	for n, f := range g.Functions {
		functionDir := gadgetDir + "/functions/" + fmt.Sprintf("%s.%s", f.Type, n)
		os.Remove(configDir + fmt.Sprintf("/%s.%s", f.Type, n))
		os.RemoveAll(functionDir)
	}

	// remove config
	os.RemoveAll(configDir)

	// remove gadget
	os.RemoveAll(gadgetDir)
}

func NewUSBGadget(name string) *USBGadget {
	g := new(USBGadget)
	g.Name = name
	g.MaxPacketSize = 64
	g.IdVendor = USB_VENDOR_ID
	g.IdProduct = USB_PRODUCT_ID
	g.UsbVersion = USB_VERSION
	g.DeviceVesion = USB_DEVICE_VERSION
	g.Strings = map[int]*USBGadgetStringDescriptor{}
	g.Strings[USB_DESC_LANG_ID] = &USBGadgetStringDescriptor{
		SerialNumber: USB_DESC_SERIAL,
		Manufacturer: USB_DESC_MANUFACTURER,
		Product:      USB_DESC_PRODUCT_NAME,
	}
	g.Functions = map[string]*USBGadgetFunction{}

	return g
}
