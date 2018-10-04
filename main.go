package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"

	"github.com/PuerkitoBio/goquery"
	"github.com/pkg/errors"
)

type configuration struct {
	Versionselector string
	Page            string
	Directories     []string
	addon           string
}

type elvui struct {
	configuration
	remoteVersion float64
	localVersion  float64
	localName     string
}

func (e *elvui) init(configPath string) error {
	rawConfig, err := ioutil.ReadFile(configPath)
	if err != nil {
		return errors.Wrapf(err, "cannot read file %s", configPath)
	}
	if err = json.Unmarshal(rawConfig, e); err != nil {
		return errors.Wrap(err, "cannot unmarshal config")
	}

	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Wow6432Node\Blizzard Entertainment\World of Warcraft`, registry.QUERY_VALUE)
	if err != nil {
		return errors.Wrap(err, "cannot find WoW install directory")
	}
	defer k.Close()

	s, _, err := k.GetStringValue("InstallPath")
	if err != nil {
		return errors.Wrap(err, "cannot find WoW install directory")
	}
	e.addon = filepath.Join(s, "Interface", "AddOns")

	return nil
}

func (e *elvui) getRemoteVersion() error {
	doc, err := goquery.NewDocument(e.Page)
	if err != nil {
		return errors.Wrapf(err, "cannot parse url: %s", e.Page)
	}

	sel := doc.Find(e.Versionselector)
	if sel == nil {
		return errors.Errorf("version not found at %s", e.Page)
	}

	span := strings.TrimSpace(sel.Text())
	if e.remoteVersion, err = strconv.ParseFloat(span, 64); err != nil {
		return errors.Wrapf(err, "cannot parse version number %s", span)
	}

	return nil
}

func (e *elvui) getLocalVersion() error {
	prefix := "## Version: "
	tocFile := filepath.Join(e.addon, e.localName, e.localName+".toc")

	toc, err := os.Open(tocFile)
	if err != nil {
		return errors.Wrapf(err, "cannot open file %s", tocFile)
	}
	defer toc.Close()
	tocReader := bufio.NewReader(toc)

	for {
		line, err := tocReader.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return errors.Wrapf(err, "cannot read lines from %s", tocFile)
		}
		if strings.HasPrefix(line, prefix) {
			// retard windows need -1
			rawVer := strings.TrimSpace(line[len(prefix) : len(line)-1])
			if e.localVersion, err = strconv.ParseFloat(rawVer, 64); err != nil {
				return errors.Wrapf(err, "cannot parse version number %s", rawVer)
			}
			return nil
		}
	}

	return errors.Errorf("local version not found at %s", tocFile)
}

func (e elvui) downloadAndExtract() error {
	dlLink := fmt.Sprintf("http://www.tukui.org/downloads/%s-%.2f.zip",
		strings.ToLower(e.localName), e.remoteVersion)

	response, err := http.Get(dlLink)
	if err != nil {
		return errors.Wrapf(err, "cannot download file url %s", dlLink)
	}
	defer response.Body.Close()
	// hope tukui don't overflow my memory
	respBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return errors.Wrap(err, "cannot read response")
	}
	readerBytes := bytes.NewReader(respBytes)
	// zip work
	zipReader, err := zip.NewReader(readerBytes, response.ContentLength)
	if err != nil {
		return errors.Wrap(err, "cannot create zip reader")
	}

	// remove older directories
	for _, dir := range e.Directories {
		addonDir := filepath.Join(e.addon, dir)
		if err := os.RemoveAll(addonDir); err != nil {
			return errors.Wrapf(err, "cannot remove directory %s", addonDir)
		}
	}

	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			addonDir := filepath.Join(e.addon, f.Name)
			if err := os.MkdirAll(addonDir, f.Mode()); err != nil {
				return errors.Wrapf(err, "cannot create directory %s", addonDir)
			}
		} else {
			// open file inside zip for copy
			fileInZip, err := f.Open()
			if err != nil {
				return errors.Wrapf(err, "cannot open file %s inside zip", f.Name)
			}
			// create local file
			localName := filepath.Join(e.addon, f.Name)
			fileLocal, err := os.Create(localName)
			if err != nil {
				return errors.Wrapf(err, "cannot create file %s", localName)
			}
			// copy contents over
			_, err = io.Copy(fileLocal, fileInZip)
			if err != nil {
				return errors.Wrapf(err, "cannot extract content from %s to %s", f.Name, localName)
			}

			fileLocal.Close()
			fileInZip.Close()
		}
	}

	return nil
}

func main() {
	conf := elvui{localName: "ElvUI"}
	if err := conf.init("config.json"); err != nil {
		log.Fatalf("Fatal: %+v\n", err)
	}

	if err := conf.getLocalVersion(); err != nil {
		log.Fatalf("Fatal: %+v\n", err)
	}
	if err := conf.getRemoteVersion(); err != nil {
		log.Fatalf("Fatal: %+v\n", err)
	}
	if conf.remoteVersion > conf.localVersion {
		log.Printf("Upgrading %.2f->%.2f\n", conf.localVersion, conf.remoteVersion)
		if err := conf.downloadAndExtract(); err != nil {
			log.Fatalf("Fatal: %+v\n", err)
		}
		log.Println("Success")
	} else {
		log.Println("Nothing to do")
	}

	log.Println("Press 'Enter' to finish...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
