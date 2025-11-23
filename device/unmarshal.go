package device

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/supersonic-app/go-upnpcast/services"
)

type dmrSchema struct {
	XMLName xml.Name `xml:"root"`
	Device  struct {
		XMLName      xml.Name `xml:"device"`
		FriendlyName string   `xml:"friendlyName"`
		ModelName    string   `xml:"modelName"`
		DeviceList   struct {
			XMLName xml.Name `xml:"deviceList"`
			Devices []struct {
				XMLName      xml.Name `xml:"device"`
				DeviceType   string   `xml:"deviceType"`
				FriendlyName string   `xml:"friendlyName"`
				ModelName    string   `xml:"modelName"`
				ServiceList  struct {
					XMLName  xml.Name `xml:"serviceList"`
					Services []struct {
						XMLName     xml.Name      `xml:"service"`
						Type        services.Type `xml:"serviceType"`
						ID          string        `xml:"serviceId"`
						ControlURL  string        `xml:"controlURL"`
						EventSubURL string        `xml:"eventSubURL"`
					} `xml:"service"`
				} `xml:"serviceList"`
			} `xml:"device"`
		} `xml:"deviceList"`
		ServiceList struct {
			XMLName  xml.Name `xml:"serviceList"`
			Services []struct {
				XMLName     xml.Name      `xml:"service"`
				Type        services.Type `xml:"serviceType"`
				ID          string        `xml:"serviceId"`
				ControlURL  string        `xml:"controlURL"`
				EventSubURL string        `xml:"eventSubURL"`
			} `xml:"service"`
		} `xml:"serviceList"`
	} `xml:"device"`
}

func mediaRendererFromDeviceURL(ctx context.Context, dmrurl string) (*MediaRenderer, error) {
	parsedURL, err := url.Parse(dmrurl)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("device URL parse error: %w", err)
	}

	log.Printf("fetching device manifest from %s", dmrurl)

	client := &http.Client{}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dmrurl, nil)
	if err != nil {
		return nil, fmt.Errorf("setup GET device manifest error: %w", err)
	}
	req.Header.Set("Connection", "close")

	xmlresp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do GET device manifest error: %w", err)
	}
	defer xmlresp.Body.Close()

	var root dmrSchema
	err = xml.NewDecoder(xmlresp.Body).Decode(&root)
	if err != nil {
		return nil, fmt.Errorf("unmarshal device manifest error: %w", err)
	}

	mr := &MediaRenderer{
		URL:          dmrurl,
		FriendlyName: root.Device.FriendlyName,
		ModelName:    root.Device.ModelName,
	}

	log.Printf("found device: %s (%s)", mr.FriendlyName, mr.ModelName)

	var servicesAgnostic = root.Device.ServiceList.Services
	if (len(servicesAgnostic) == 0) && (len(root.Device.DeviceList.Devices) > 0) {
		// look for MediaRenderer device in sub-devices if services not found at top level
		for i := 0; i < len(root.Device.DeviceList.Devices); i++ {
			subDevice := root.Device.DeviceList.Devices[i]
			if subDevice.DeviceType != "urn:schemas-upnp-org:device:MediaRenderer:1" {
				continue
			}
			servicesAgnostic = subDevice.ServiceList.Services
			break
		}
	}
	for i := 0; i < len(servicesAgnostic); i++ {
		// normalize service URLs to start with leading /
		service := servicesAgnostic[i]
		if !strings.HasPrefix(service.EventSubURL, "/") {
			service.EventSubURL = "/" + service.EventSubURL
		}
		if !strings.HasPrefix(service.ControlURL, "/") {
			service.ControlURL = "/" + service.ControlURL
		}

		switch service.Type {
		case services.AVTransport:
			mr.avTransportControlURL = parsedURL.Scheme + "://" + parsedURL.Host + service.ControlURL
			mr.avTransportEventSubURL = parsedURL.Scheme + "://" + parsedURL.Host + service.EventSubURL

			if _, err := url.ParseRequestURI(mr.avTransportControlURL); err != nil {
				return nil, fmt.Errorf("invalid AVTransportControlURL: %w", err)
			}

			if _, err = url.ParseRequestURI(mr.avTransportEventSubURL); err != nil {
				return nil, fmt.Errorf("invalid AVTransportEventSubURL: %w", err)
			}
		case services.RenderingControl:
			mr.renderingControlURL = parsedURL.Scheme + "://" + parsedURL.Host + service.ControlURL

			_, err = url.ParseRequestURI(mr.renderingControlURL)
			if err != nil {
				return nil, fmt.Errorf("invalid RenderingControlURL: %w", err)
			}
		case services.ConnectionManager:
			mr.connectionManagerURL = parsedURL.Scheme + "://" + parsedURL.Host + service.ControlURL
			if err != nil {
				return nil, fmt.Errorf("invalid ConnectionManagerURL: %w", err)
			}
		}
	}

	if mr.avTransportControlURL != "" {
		return mr, nil
	}

	return nil, errors.New("wrong DMR")
}
