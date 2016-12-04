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

	"github.com/PuerkitoBio/goquery"
)

type configuration struct {
	Versionselector string
	Page            string
	Directories     []string
	Addon           string
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
		return err
	}
	return json.Unmarshal(rawConfig, e)
}

func (e *elvui) getRemoteVersion() error {
	doc, err := goquery.NewDocument(e.Page)
	if err != nil {
		return err
	}

	sel := doc.Find(e.Versionselector)
	if sel == nil {
		return fmt.Errorf("Version not found at %s", e.Page)
	}

	span := sel.Text()
	if e.remoteVersion, err = strconv.ParseFloat(span, 64); err != nil {
		return err
	}

	return nil
}

func (e *elvui) getLocalVersion() error {
	prefix := "## Version: "
	tocFile := filepath.Join(e.Addon, e.localName, e.localName+".toc")

	toc, err := os.Open(tocFile)
	if err != nil {
		return err
	}
	defer toc.Close()
	tocReader := bufio.NewReader(toc)

	for {
		line, err := tocReader.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return err
		}
		if strings.HasPrefix(line, prefix) {
			// retard windows need -1
			if e.localVersion, err = strconv.ParseFloat(line[len(prefix):len(line)-1], 64); err != nil {
				return err
			}
			return nil
		}
	}

	return fmt.Errorf("Local version not found at %s", tocFile)
}

func (e elvui) downloadAndExtract() error {
	dlLink := fmt.Sprintf("http://www.tukui.org/downloads/%s-%.2f.zip",
		strings.ToLower(e.localName), e.remoteVersion)

	response, err := http.Get(dlLink)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	// hope tukui don't overflow my memory
	respBytes, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return err
	}
	readerBytes := bytes.NewReader(respBytes)
	// zip work
	zipReader, err := zip.NewReader(readerBytes, response.ContentLength)
	if err != nil {
		return err
	}

	// remove older directories
	for _, dir := range e.Directories {
		if err := os.RemoveAll(filepath.Join(e.Addon, dir)); err != nil {
			return err
		}
	}

	for _, f := range zipReader.File {
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(filepath.Join(e.Addon, f.Name), f.Mode()); err != nil {
				return err
			}
		} else {
			// open file inside zip for copy
			fileInZip, err := f.Open()
			if err != nil {
				return err
			}
			// create local file
			fileLocal, err := os.Create(filepath.Join(e.Addon, f.Name))
			if err != nil {
				return err
			}
			// copy contents over
			_, err = io.Copy(fileLocal, fileInZip)
			if err != nil {
				return err
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
		log.Fatal(err)
	}

	if err := conf.getLocalVersion(); err != nil {
		log.Fatal(err)
	}
	if err := conf.getRemoteVersion(); err != nil {
		log.Fatal(err)
	}
	if conf.remoteVersion > conf.localVersion {
		log.Printf("Upgrading %.2f->%.2f\n", conf.localVersion, conf.remoteVersion)
		if err := conf.downloadAndExtract(); err != nil {
			log.Fatal(err)
		}
		log.Println("Success")
	} else {
		log.Println("Nothing to do")
	}

}
