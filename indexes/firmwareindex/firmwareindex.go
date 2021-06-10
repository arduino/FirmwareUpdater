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

package firmwareindex

import (
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/arduino/arduino-cli/arduino/security"
	"github.com/arduino/go-paths-helper"
	rice "github.com/cmaglie/go.rice"
	"github.com/sirupsen/logrus"
	semver "go.bug.st/relaxed-semver"
)

// Index represents Boards struct as seen from module_firmware_index.json file.
type Index struct {
	Boards    []*IndexBoard
	IsTrusted bool
}

// indexPackage represents a single entry from module_firmware_index.json file.
type IndexBoard struct {
	Fqbn            string                `json:"fqbn,required"`
	Firmwares       []*IndexFirmware      `json:"firmware,required"`
	LoaderSketch    *IndexLoaderSketch    `json:"loader_sketch,required"`
	Module          string                `json:"module,required"`
	Name            string                `json:"name,required"`
	Uploader        string                `json:"uploader,required"`
	UploadTouch     bool                  `json:"upload.use_1200bps_touch"`
	UploadWait      bool                  `json:"upload.wait_for_upload_port"`
	UploaderCommand *IndexUploaderCommand `json:"uploader.command,required"`
}

type IndexUploaderCommand struct {
	Linux   string `json:"linux,required"`
	Windows string `json:"windows"`
	Macosx  string `json:"macosx"`
}

// IndexFirmware represents a single Firmware version from module_firmware_index.json file.
type IndexFirmware struct {
	Version  string      `json:"version,required"` // `*semver.Version` but with SARA version is giving problems
	URL      string      `json:"url,required"`
	Checksum string      `json:"checksum,required"`
	Size     json.Number `json:"size,required"`
	Module   string      `json:"module,required"`
}

// IndexLoaderSketch represents the sketch used to upload the new firmware on a board.
type IndexLoaderSketch struct {
	URL      string      `json:"url,required"`
	Checksum string      `json:"checksum,required"`
	Size     json.Number `json:"size,required"`
}

// LoadIndex reads a module_firmware_index.json from a file and returns the corresponding Index structure.
func LoadIndex(jsonIndexFile *paths.Path) (*Index, error) {
	buff, err := jsonIndexFile.ReadFile()
	if err != nil {
		return nil, err
	}
	var index Index
	err = json.Unmarshal(buff, &index.Boards)
	if err != nil {
		return nil, err
	}

	jsonSignatureFile := jsonIndexFile.Parent().Join(jsonIndexFile.Base() + ".sig")
	keysBox, err := rice.FindBox("gpg_keys")
	if err != nil {
		return nil, err
	}
	key, err := keysBox.Open("module_firmware_index_public.gpg.key")
	if err != nil {
		return nil, err
	}
	trusted, _, err := security.VerifySignature(jsonIndexFile, jsonSignatureFile, key)
	if err != nil {
		logrus.
			WithField("index", jsonIndexFile).
			WithField("signatureFile", jsonSignatureFile).
			WithError(err).Infof("Checking signature")
	} else {
		logrus.
			WithField("index", jsonIndexFile).
			WithField("signatureFile", jsonSignatureFile).
			WithField("trusted", trusted).Infof("Checking signature")
		index.IsTrusted = trusted
	}
	return &index, nil
}

// LoadIndexNoSign reads a module_firmware_index.json from a file and returns the corresponding Index structure.
func LoadIndexNoSign(jsonIndexFile *paths.Path) (*Index, error) {
	buff, err := jsonIndexFile.ReadFile()
	if err != nil {
		return nil, err
	}
	var index Index
	err = json.Unmarshal(buff, &index.Boards)
	if err != nil {
		return nil, err
	}

	index.IsTrusted = true

	return &index, nil
}

// GetLatestFirmwareURL takes the fqbn as parameter and returns the URL of the latest available firmware.
// Not currently implemented for SARA, as the version for it's firmware is a bit strange
func (i *Index) GetLatestFirmwareURL(fqbn string) (string, error) {
	board := i.GetBoard(fqbn)
	if board == nil {
		return "", fmt.Errorf("invalid FQBN: %s", fqbn)
	}
	if board.Module == "SARA" { // TODO togliere sara, lo assumo giá nel comando
		// TODO implement?? by defualt you have to specify the version
		return "", fmt.Errorf("not implemented for SARA module")
	}

	var latestVersion *semver.RelaxedVersion
	var latestFirmwareURL string
	for _, firmware := range board.Firmwares {
		version := semver.ParseRelaxed(firmware.Version)
		if latestVersion == nil || version.GreaterThan(latestVersion) { // TODO check the condition
			latestVersion = version
			latestFirmwareURL = firmware.URL
		}
	}
	if latestVersion != nil {
		return latestFirmwareURL, nil
	} else {
		return "", fmt.Errorf("cannot find latest version")
	}
}

// GetFirmwareURL will take the fqbn of the required board and the version of the firmware as parameters.
// It will return the URL of the required firmware
func (i *Index) GetFirmwareURL(fqbn, version string) (string, error) {
	board := i.GetBoard(fqbn)
	if board == nil {
		return "", fmt.Errorf("invalid FQBN: %s", fqbn)
	}
	for _, firmware := range board.Firmwares {
		if firmware.Version == version {
			return firmware.URL, nil
		}
	}
	return "", fmt.Errorf("invalid version: %s", version)
}

// GetLoaderSketchURL will take the board's fqbn and return the url of the loader sketch
func (i *Index) GetLoaderSketchURL(fqbn string) (string, error) {
	board := i.GetBoard(fqbn)
	if board == nil {
		return "", fmt.Errorf("invalid FQBN: %s", fqbn)
	}
	return board.LoaderSketch.URL, nil
}

// GetBoard returns the IndexBoard for the given FQBN
func (i *Index) GetBoard(fqbn string) *IndexBoard {
	for _, b := range i.Boards {
		if b.Fqbn == fqbn {
			return b
		}
	}
	return nil
}

func (b *IndexBoard) GetUploaderCommand() string {
	if runtime.GOOS == "windows" && b.UploaderCommand.Windows != "" {
		return b.UploaderCommand.Linux
	} else if runtime.GOOS == "darwin" && b.UploaderCommand.Macosx != "" {
		return b.UploaderCommand.Macosx
	}
	// The linux uploader command is considere to be the generic one
	return b.UploaderCommand.Linux
}

// GetModule will take the board's fqbn and return the name of the module
func (i *Index) GetModule(fqbn string) (string, error) {
	for _, board := range i.Boards {
		if board.Fqbn == fqbn {
			return board.Module, nil
		}
	}
	return "", fmt.Errorf("invalid FQBN: %s", fqbn)
}