/*
  FirmwareUploader
  Copyright (c) 2021 Arduino LLC.  All right reserved.

  This library is free software; you can redistribute it and/or
  modify it under the terms of the GNU Lesser General Public
  License as published by the Free Software Foundation; either
  version 2.1 of the License, or (at your option) any later version.

  This library is distributed in the hope that it will be useful,
  but WITHOUT ANY WARRANTY; without even the implied warranty of
  MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU
  Lesser General Public License for more details.

  You should have received a copy of the GNU Lesser General Public
  License along with this library; if not, write to the Free Software
  Foundation, Inc., 51 Franklin St, Fifth Floor, Boston, MA  02110-1301  USA
*/

package flasher

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/arduino/go-paths-helper"
	"go.bug.st/serial"
)

func NewWincFlasher(portAddress string) (*WincFlasher, error) {
	port, err := openSerial(portAddress)
	if err != nil {
		return nil, err
	}
	f := &WincFlasher{port: port}
	payloadSize, err := f.getMaximumPayloadSize()
	if err != nil {
		return nil, err
	}
	if payloadSize < 1024 {
		return nil, fmt.Errorf("programmer reports %d as maximum payload size (1024 is needed)", payloadSize)
	}
	f.payloadSize = int(payloadSize)
	return f, nil
}

type WincFlasher struct {
	port        serial.Port
	payloadSize int
}

func (f *WincFlasher) FlashFirmware(firmwareFile *paths.Path) error {
	// log.Printf("Flashing firmware from '%v'", ctx.FirmwareFile)
	data, err := firmwareFile.ReadFile()
	if err != nil {
		return err
	}
	firmwareOffset := 0x0000
	return f.flashChunk(firmwareOffset, data)
}

func (f *WincFlasher) FlashCertificates(certificatePaths *paths.PathList) error {
	// TODO
	return nil
}

func (f *WincFlasher) Close() error {
	return f.port.Close()
}

func (f *WincFlasher) hello() error {
	// "HELLO" command
	err := f.sendCommand(CommandData{
		Command: 0x99,
		Address: 0x11223344,
		Value:   0x55667788,
		Payload: nil,
	})
	if err != nil {
		return err
	}

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Receive response
	res := make([]byte, 65535)
	n, err := f.port.Read(res)
	if err != nil {
		return err
	}
	// flush eventual leftover from the rx buffer
	if n >= 6 {
		res = res[n-6 : n]
	}

	if res[0] != 'v' {
		return FlasherError{err: "Programmer is not responding"}
	}
	if string(res) != "v10000" {
		// TODO: Do we really need this check? What is it trying to verify?
		return FlasherError{err: fmt.Sprintf("Programmer version mismatch, v10000 needed: %s", res)}
	}
	return nil
}

func (f *WincFlasher) write(address uint32, buffer []byte) error {
	// "FLASH_WRITE" command
	err := f.sendCommand(CommandData{
		Command: 0x02,
		Address: address,
		Value:   0,
		Payload: buffer,
	})
	if err != nil {
		return err
	}

	// wait acknowledge
	ack := make([]byte, 2)
	if err := f.serialFillBuffer(ack); err != nil {
		return err
	}
	if string(ack) != "OK" {
		return FlasherError{err: fmt.Sprintf("Missing ack on write: %s", ack)}
	}
	return nil
}

func (f *WincFlasher) flashChunk(offset int, buffer []byte) error {
	bufferLength := len(buffer)

	if err := f.erase(uint32(offset), uint32(bufferLength)); err != nil {
		return err
	}

	for i := 0; i < bufferLength; i += f.payloadSize {
		fmt.Printf("\rFlashing: " + strconv.Itoa((i*100)/bufferLength) + "%%")
		start := i
		end := i + f.payloadSize
		if end > bufferLength {
			end = bufferLength
		}
		if err := f.write(uint32(offset+i), buffer[start:end]); err != nil {
			return err
		}
	}

	var flashData []byte
	for i := 0; i < bufferLength; i += f.payloadSize {
		readLength := f.payloadSize
		if (i + f.payloadSize) > bufferLength {
			readLength = bufferLength % f.payloadSize
		}

		data, err := f.read(uint32(offset+i), uint32(readLength))
		if err != nil {
			return err
		}

		flashData = append(flashData, data...)
	}

	if !bytes.Equal(buffer, flashData) {
		return errors.New("flash data does not match written")
	}

	return nil
}

func (f *WincFlasher) getMaximumPayloadSize() (uint16, error) {
	// "MAX_PAYLOAD_SIZE" command
	err := f.sendCommand(CommandData{
		Command: 0x50,
		Address: 0,
		Value:   0,
		Payload: nil,
	})
	if err != nil {
		return 0, err
	}

	// Receive response
	res := make([]byte, 2)
	if err := f.serialFillBuffer(res); err != nil {
		return 0, err
	}
	return (uint16(res[0]) << 8) + uint16(res[1]), nil
}

func (f *WincFlasher) serialFillBuffer(buffer []byte) error {
	read := 0
	for read < len(buffer) {
		n, err := f.port.Read(buffer[read:])
		if err != nil {
			return err
		}
		if n == 0 {
			return &FlasherError{err: "Serial port closed unexpectedly"}
		}
		read += n
	}
	return nil
}

func (f *WincFlasher) sendCommand(data CommandData) error {
	buff := new(bytes.Buffer)
	if err := binary.Write(buff, binary.BigEndian, data.Command); err != nil {
		return err
	}
	if err := binary.Write(buff, binary.BigEndian, data.Address); err != nil {
		return err
	}
	if err := binary.Write(buff, binary.BigEndian, data.Value); err != nil {
		return err
	}
	var length uint16
	if data.Payload == nil {
		length = 0
	} else {
		length = uint16(len(data.Payload))
	}
	if err := binary.Write(buff, binary.BigEndian, length); err != nil {
		return err
	}
	if data.Payload != nil {
		buff.Write(data.Payload)
	}

	bufferData := buff.Bytes()
	for {
		sent, err := f.port.Write(bufferData)
		if err != nil {
			return err
		}
		if sent == len(bufferData) {
			break
		}
		// fmt.Println("HEY! sent", sent, "out of", len(bufferData))
		bufferData = bufferData[sent:]
	}
	return nil
}

// Read a block of flash memory
func (f *WincFlasher) read(address uint32, length uint32) ([]byte, error) {
	// "FLASH_READ" command
	err := f.sendCommand(CommandData{
		Command: 0x01,
		Address: address,
		Value:   length,
		Payload: nil,
	})
	if err != nil {
		return nil, err
	}

	// Receive response
	result := make([]byte, length)
	if err := f.serialFillBuffer(result); err != nil {
		return nil, err
	}
	ack := make([]byte, 2)
	if err := f.serialFillBuffer(ack); err != nil {
		return nil, err
	}
	if string(ack) != "OK" {
		return nil, FlasherError{err: fmt.Sprintf("Missing ack on read: %s", ack)}
	}
	return result, nil
}

// Erase a block of flash memory
func (f *WincFlasher) erase(address uint32, length uint32) error {
	// "FLASH_ERASE" command
	err := f.sendCommand(CommandData{
		Command: 0x03,
		Address: address,
		Value:   length,
		Payload: nil,
	})
	if err != nil {
		return err
	}

	log.Printf("Erasing %d bytes from address 0x%X\n", length, address)

	// wait acknowledge
	ack := make([]byte, 2)
	if err := f.serialFillBuffer(ack); err != nil {
		return err
	}
	if string(ack) != "OK" {
		return FlasherError{err: fmt.Sprintf("Missing ack on erase: %s", ack)}
	}
	return nil
}
