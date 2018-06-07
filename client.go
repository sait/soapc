package soap

import (
	"bytes"
	"crypto/tls"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"
)

// Envelope envelope
type Envelope struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Header  *Header  `xml:",omitempty"`
	Body    Body
}

// Header header
type Header struct {
	XMLName xml.Name    `xml:"http://schemas.xmlsoap.org/soap/envelope/ Header"`
	Content interface{} `xml:",omitempty"`
}

// Body body
type Body struct {
	XMLName xml.Name    `xml:"http://schemas.xmlsoap.org/soap/envelope/ Body"`
	Fault   *Fault      `xml:",omitempty"`
	Content interface{} `xml:",omitempty"`
}

// Fault fault
type Fault struct {
	XMLName xml.Name `xml:"http://schemas.xmlsoap.org/soap/envelope/ Fault"`
	Code    string   `xml:"faultcode,omitempty"`
	String  string   `xml:"faultstring,omitempty"`
	Actor   string   `xml:"faultactor,omitempty"`
	Detail  string   `xml:"detail,omitempty"`
}

func (f *Fault) Error() string {
	return f.String
}

// NewClient return SOAP client
func NewClient(url string, tls bool, header interface{}) *Client {
	return &Client{
		url:    url,
		tls:    tls,
		header: header,
	}
}

// Client SOAP client
type Client struct {
	url       string
	tls       bool
	userAgent string
	header    interface{}
}

func dialTimeout(network, addr string) (net.Conn, error) {
	timeout := time.Duration(30 * time.Second)
	return net.DialTimeout(network, addr, timeout)
}

// UnmarshalXML unmarshal SOAPHeader
func (h *Header) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var (
		token xml.Token
		err   error
	)
Loop:
	for {
		if token, err = d.Token(); err != nil {
			return err
		}
		if token == nil {
			break
		}
		switch se := token.(type) {
		case xml.StartElement:
			if err = d.DecodeElement(h.Content, &se); err != nil {
				return err
			}
		case xml.EndElement:
			break Loop
		}
	}
	return nil
}

// UnmarshalXML unmarshal SOAPBody
func (b *Body) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	if b.Content == nil {
		return xml.UnmarshalError("Content must be a pointer to a struct")
	}
	var (
		token    xml.Token
		err      error
		consumed bool
	)
Loop:
	for {
		if token, err = d.Token(); err != nil {
			return err
		}
		if token == nil {
			break
		}
		envelopeNameSpace := "http://schemas.xmlsoap.org/soap/envelope/"
		switch se := token.(type) {
		case xml.StartElement:
			if consumed {
				return xml.UnmarshalError(
					"Found multiple elements inside SOAP body; not wrapped-document/literal WS-I compliant")
			} else if se.Name.Space == envelopeNameSpace && se.Name.Local == "Fault" {
				b.Fault = &Fault{}
				b.Content = nil
				err = d.DecodeElement(b.Fault, &se)
				if err != nil {
					return err
				}
				consumed = true
			} else {
				if err = d.DecodeElement(b.Content, &se); err != nil {
					return err
				}
				consumed = true
			}
		case xml.EndElement:
			break Loop
		}
	}
	return nil
}

// Call SOAP client API call
func (s *Client) Call(soapAction string, request interface{}) (response []byte, err error) {
	var envelope Envelope
	if s.header != nil {
		envelope = Envelope{
			Header: &Header{
				Content: s.header,
			},
			Body: Body{
				Content: request,
			},
		}
	} else {
		envelope = Envelope{
			Body: Body{
				Content: request,
			},
		}
	}

	buffer := new(bytes.Buffer)
	buffer.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	encoder := xml.NewEncoder(buffer)
	encoder.Indent("  ", "    ")
	if err = encoder.Encode(envelope); err != nil {
		err = fmt.Errorf("failed to encode envelope: %s", err.Error())
		return
	}
	if err = encoder.Flush(); err != nil {
		err = fmt.Errorf("failed to flush encoder: %s", err.Error())
		return
	}

	req, err := http.NewRequest("POST", s.url, buffer)
	if err != nil {
		err = fmt.Errorf("failed to create POST request: %s", err.Error())
		return
	}
	req.Header.Add("Content-Type", "text/xml; charset=\"utf-8\"")
	req.Header.Set("SOAPAction", soapAction)
	req.Header.Set("Content-Length", string(buffer.Len()))
	req.Header.Set("User-Agent", s.userAgent)
	req.Close = true

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: s.tls,
		},
		Dial: dialTimeout,
	}

	client := &http.Client{Transport: tr}
	res, err := client.Do(req)
	if err != nil {
		err = fmt.Errorf("failed to send SOAP request: %s", err.Error())
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		soapFault, errr := ioutil.ReadAll(res.Body)
		if errr != nil {
			err = fmt.Errorf("failed to read SOAP fault response body: %s", errr.Error())
		}
		err = fmt.Errorf("HTTP Status Code: %d, SOAP Fault: \n%s", res.StatusCode, string(soapFault))
		return
	}

	response, err = ioutil.ReadAll(res.Body)
	if err != nil {
		err = fmt.Errorf("failed to read SOAP body: %s", err.Error())
		return
	}
	if len(response) == 0 {
		return
	}
	return
}
