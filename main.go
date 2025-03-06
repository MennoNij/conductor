package main

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	"math"
	"time"

	"tinygo.org/x/bluetooth"
)

// Define the BMS characteristic ID (replace with the correct UUID from your BMS)
var BMS_CHARACTERISTIC_ID string = "0000ffe1-0000-1000-8000-00805f9b34fb" // bluetooth.NewUUID([16]byte{0x00, 0x00, 0x04, 0x01, 0x13, 0x55, 0xAA, 0x17, 0x00, 0x00, 0x10, 0x00, 0x80, 0x00, 0x00, 0x00})
var SN_CHARACTERISTIC_ID string = "0000ffe2-0000-1000-8000-00805f9b34fb"

// BMS command set
var pqCommands = map[string]string{
	"GET_VERSION":      "000004011655AA1A",
	"GET_BATTERY_INFO": "000004011355AA17",
	"SERIAL_NUMBER":    "000004011055AA14",
}

// Define a struct to hold battery data
type BatteryInfo struct {
	PackVoltage          int
	Voltage              int
	BatteryPack          map[int]float64
	Current              float64
	Watt                 float64
	RemainAh             float64
	FactoryAh            float64
	CellTemperature      int
	MosfetTemperature    int
	Heat                 string
	DischargeSwitchState int
	ProtectState         string
	FailureState         []byte
	EquilibriumState     int
	BatteryState         int
	SOC                  int
	SOH                  int
	DischargesCount      int
	DischargesAHCount    int
	BatteryStatus        string
	BalanceStatus        string
	CellStatus           string
	HeatStatus           string
}

// Helper function to convert byte array to integer with big-endian byte order
func reverseBytes(data []byte) []byte {
	reversed := make([]byte, len(data))
	for i, v := range data {
		reversed[len(data)-i-1] = v
	}
	return reversed
}

// Helper function to convert byte array to integer with big-endian byte order
func binaryToIntBigEndian(data []byte) int {
	if len(data) != 4 {
		log.Fatalf("Expected 4 bytes but got %d bytes", len(data))
	}
	// Reverse bytes to match Python's [::-1] behavior
	reversedData := reverseBytes(data)
	// Now read the reversed bytes as big-endian
	return int(binary.BigEndian.Uint32(reversedData))
}

func parseBatteryInfo(data []byte) *BatteryInfo {
	battery := &BatteryInfo{
		BatteryPack: make(map[int]float64),
	}

	// Parse the PackVoltage and Voltage
	fmt.Println("RAW VOLTAGE: ", hex.EncodeToString(data[12:16]))
	battery.PackVoltage = binaryToIntBigEndian(data[8:12]) //int(binaryToInt(data[8:12]))
	battery.Voltage = binaryToIntBigEndian(data[12:16])    //int(binaryToInt(data[12:16]))

	// Parse the battery pack voltages (every 2 bytes is a cell voltage)
	batPack := data[16:48]
	for i := 0; i < len(batPack); i += 2 {
		cellVoltage := int(binaryToInt([]byte{batPack[i+1], batPack[i]}))
		if cellVoltage == 0 {
			continue
		}
		cell := i/2 + 1
		battery.BatteryPack[cell] = float64(cellVoltage) / 1000
	}

	// Parse current in Amperes (signed)
	current := int(binaryToInt(data[48:52]))
	battery.Current = float64(current) / 1000

	// Calculate load/unload wattage
	battery.Watt = math.Round((float64(battery.Voltage)*battery.Current)/10000*100) / 100

	// Parse remaining Ah and factory Ah
	battery.RemainAh = float64(binaryToInt(data[62:64])) / 100
	battery.FactoryAh = float64(binaryToInt(data[64:66])) / 100

	// Parse temperature values
	battery.CellTemperature = int(binaryToInt(data[52:54]))
	battery.MosfetTemperature = int(binaryToInt(data[54:56]))

	// Parse heat status and discharge switch state
	battery.Heat = hex.EncodeToString(data[68:72])
	if data[68]>>7 >= 8 {
		battery.DischargeSwitchState = 0
	} else {
		battery.DischargeSwitchState = 1
	}

	// Parse the protect, failure states, and equilibrium state
	battery.ProtectState = hex.EncodeToString(data[76:80])
	battery.FailureState = data[80:84]
	battery.EquilibriumState = int(binaryToInt(data[84:88]))

	// Parse battery state, SOC, SOH, discharge counts
	battery.BatteryState = int(binaryToInt(data[88:90]))
	battery.SOC = int(binaryToInt(data[90:92]))
	battery.SOH = int(binaryToInt(data[92:96]))
	battery.DischargesCount = int(binaryToInt(data[96:100]))
	battery.DischargesAHCount = int(binaryToInt(data[100:104]))

	// Determine battery status
	battery.BatteryStatus = getBatteryStatus(battery)

	// Balance status
	if battery.EquilibriumState > 0 {
		battery.BalanceStatus = "Battery cells are being balanced for better performance."
	} else {
		battery.BalanceStatus = "All cells are well-balanced."
	}

	// Cell status
	if battery.FailureState[0] > 0 || battery.FailureState[1] > 0 {
		battery.CellStatus = "Fault alert! There may be a problem with cell."
	} else {
		battery.CellStatus = "Battery is in optimal working condition."
	}

	// Heat status
	if battery.Heat[7] == '2' {
		battery.HeatStatus = "Self-heating is on"
	} else {
		battery.HeatStatus = "Self-heating is off"
	}

	return battery
}

// Convert a byte slice to an integer in big-endian order
func binaryToInt(data []byte) int64 {
	var result int64
	for _, b := range data {
		result = (result << 8) | int64(b)
	}
	return result
}

func getBatteryStatus(battery *BatteryInfo) string {
	var status string
	if battery.Current == 0 {
		status = "Standby"
	} else if battery.Current > 0 {
		status = "Charging"
	} else if battery.Current < 0 {
		status = "Discharging"
	}

	if battery.SOC >= 100 || battery.BatteryState == 4 {
		status = "Full Charge"
	}

	return status
}

func parseVersion(data []byte) (string, string, string) {
	start := data[8:]

	// Firmware version
	firmwareVersion := fmt.Sprintf("%d.%d.%d", binaryToInt(start[0:2]), binaryToInt(start[2:4]), binaryToInt(start[4:6]))

	// Manufacture date
	manufactureDate := fmt.Sprintf("%d-%d-%d", binaryToInt(start[6:8]), start[8], start[9])

	// Hardware version as a string
	var hardwareVersion string
	for i := 0; i < len(start); i += 2 { // Loop with a step of 2
		ver := start[i]
		if ver >= 32 && ver <= 126 {
			hardwareVersion += string(ver)
		}
	}

	return firmwareVersion, manufactureDate, hardwareVersion
}

var adapter = bluetooth.DefaultAdapter

func main() {
	fmt.Println("BMS_CHARACTERISTIC_ID:", BMS_CHARACTERISTIC_ID)
	// Enable the Bluetooth adapter
	if err := adapter.Enable(); err != nil {
		log.Fatalf("Failed to enable Bluetooth: %v", err)
	}

	fmt.Println("Scanning for BMS device...")

	// Scan for devices
	err := adapter.Scan(func(adapter *bluetooth.Adapter, device bluetooth.ScanResult) {
		fmt.Printf("Found device: %s [%s]\n", device.LocalName(), device.Address.String())

		// Connect to the BMS device (Assuming we identify it by name)
		if device.LocalName() == "P-24100BNN160-A00714" { // Replace with actual BMS name
			adapter.StopScan()

			dev, err := adapter.Connect(device.Address, bluetooth.ConnectionParams{})
			if err != nil {
				fmt.Printf("Failed to connect: %v\n", err)
				return
			}

			fmt.Println("Connected to BMS!")

			// Get the primary service
			services, err := dev.DiscoverServices(nil)
			if err != nil {
				log.Fatalf("Failed to discover services: %v", err)
			}

			for _, service := range services {
				// Find the correct characteristic
				characteristics, err := service.DiscoverCharacteristics(nil)
				if err != nil {
					log.Fatalf("Failed to discover characteristics: %v", err)
				}

				for _, char := range characteristics {
					fmt.Println("Found characteristic:", char.UUID().String())
					if char.UUID().String() == BMS_CHARACTERISTIC_ID {
						fmt.Println("Found BMS characteristic, polling data...")

						// Send commands and read responses
						readBMS(&dev, &char)
					}
				}
			}

			// Disconnect after polling
			dev.Disconnect()
			fmt.Println("Disconnected.")
		}
	})

	if err != nil {
		log.Fatalf("Error scanning: %v", err)
	}

	// Keep the program running
	select {}
}

func handleNotification(buf []byte) {
	fmt.Printf("Received Notification: %X\n", buf)
}
func readBMS(dev *bluetooth.Device, char *bluetooth.DeviceCharacteristic) {
	for name, cmd := range pqCommands {
		// Use a closure to handle the received data and call the correct parsing function
		notificationHandler := func(data []byte) {
			// Call the correct parser function
			if name == "GET_BATTERY_INFO" {
				batteryInfo := parseBatteryInfo(data)

				// Display parsed battery information (for demonstration)
				// if batteryInfo != nil {
				fmt.Printf("Parsed battery info: %+v\n", batteryInfo)
				// } else {
				// fmt.Println("Failed to parse battery info.")
			}
		}
		// Convert hex string to byte slice
		commandBytes, err := hex.DecodeString(cmd)
		if err != nil {
			fmt.Printf("Invalid command format for %s: %v\n", name, err)
			continue
		}

		// Enable notifications (only returns an error)
		// err = char.EnableNotifications(handleNotification)
		err = char.EnableNotifications(notificationHandler)
		if err != nil {
			fmt.Printf("Failed to enable notifications for %s: %v\n", name, err)
			continue
		}

		// Write command to the characteristic (without response)
		_, err = char.WriteWithoutResponse(commandBytes)
		if err != nil {
			fmt.Printf("Failed to send command %s: %v\n", name, err)
			continue
		}

		fmt.Printf("Sent command: %s\n", name)

		// Wait for response via notification
		time.Sleep(1 * time.Second) // Adjust timing if necessary

		err = char.EnableNotifications(nil)
		if err != nil {
			fmt.Printf("Failed to close notifications for %s: %v\n", name, err)
			continue
		}
	}
}

func readBMSOld(dev *bluetooth.Device, char *bluetooth.DeviceCharacteristic) {
	for name, cmd := range pqCommands {
		// Convert hex string to byte slice
		commandBytes, err := hex.DecodeString(cmd)
		if err != nil {
			fmt.Printf("Invalid command format for %s: %v\n", name, err)
			continue
		}

		// Write command to the characteristic
		_, err = char.WriteWithoutResponse(commandBytes)
		if err != nil {
			fmt.Printf("Failed to send command %s: %v\n", name, err)
			continue
		}

		// Wait for response
		time.Sleep(1 * time.Second) // Adjust as needed
		response := make([]byte, 20)
		// Read response
		_, err = char.Read(response)
		if err != nil {
			fmt.Printf("Failed to read response for %s: %v\n", name, err)
			continue
		}

		fmt.Printf("Response for %s: %X\n", name, response)
	}
}
