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

package fwindex

import (
	"encoding/json"

	"github.com/arduino/arduino-cli/arduino/security"
	"github.com/arduino/go-paths-helper"
	rice "github.com/cmaglie/go.rice"
	"github.com/sirupsen/logrus"
)

// Index represents Boards struct as seen from module_firmware_index.json file.
type Index struct {
	Boards    []indexBoard `json:"-"`
	IsTrusted bool
}

// indexPackage represents a single entry from module_firmware_index.json file.
type indexBoard struct {
	Fqbn            string            `json:"fqbn"`
	Firmwares       []indexFirmware   `json:"firmware"`
	LoaderSketch    indexLoaderSketch `json:"loader_sketch"`
	Module          string            `json:"module"`
	Name            string            `json:"name"`
	Uploader        string            `json:"uploader"`
	UploadTouch     string            `json:"upload.use_1200bps_touch"`    // TODO replace "true" with true in json otherwise is a string and not a bool
	UploadWait      string            `json:"upload.wait_for_upload_port"` // TODO see above
	UploaderCommand string            `json:"uploader.command"`
}

// indexFirmware represents a single Firmware version from module_firmware_index.json file.
type indexFirmware struct {
	Version  string      `json:"version"` // use `*semver.Version` instead but SARA version is giving problems
	URL      string      `json:"url"`
	Checksum string      `json:"checksum"`
	Size     json.Number `json:"size"`
}

// indexLoaderSketch represents the sketch used to upload the new firmware on a board.
type indexLoaderSketch struct {
	URL      string      `json:"url"`
	Checksum string      `json:"checksum"`
	Size     json.Number `json:"size"`
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
	keysBox, err := rice.FindBox("../indexes/gpg_keys")
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