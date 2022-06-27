package adapters

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"reflect"
	"sync"
	"unsafe"

	"github.com/lucas-clemente/quic-go"
	"github.com/lucas-clemente/quic-go/http3"
	"github.com/lucas-clemente/quic-go/quicvarint"
	webtransport "github.com/marten-seemann/webtransport-go"
)

type StreamMessageType = byte

const (
	messageTypeControl StreamMessageType = iota
	messageTypePing
	messageTypePong
)

type WebTransportAdapter struct {
	socketBase
	qconn     quic.Connection
	conn      *webtransport.Conn
	streamId  uint64
	stream    webtransport.Stream
	isServer  bool
	wg        *sync.WaitGroup
	readyWait *sync.WaitGroup
	isReady   bool

	lastServeError           error
	lastServeErrorUnexpected bool
}

var _ SocketAdapter = &WebTransportAdapter{}

func getPrivateField(iface interface{}, fieldName string) interface{} {
	field := reflect.ValueOf(iface).Elem().FieldByName(fieldName)
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface()
}

func getStreamID(stream webtransport.Stream) uint64 {
	sendStream := getPrivateField(stream, "SendStream")
	quicStream := getPrivateField(sendStream, "str").(http3.Stream)
	return uint64(quicStream.StreamID())
}

func getQuicConnection(conn *webtransport.Conn) quic.Connection {
	return getPrivateField(conn, "qconn").(quic.Connection)
}

func NewWebTransportAdapter(conn *webtransport.Conn, isServer bool) *WebTransportAdapter {
	adapter := &WebTransportAdapter{
		conn:      conn,
		qconn:     getQuicConnection(conn),
		isServer:  isServer,
		readyWait: &sync.WaitGroup{},
		wg:        &sync.WaitGroup{},
		isReady:   false,
	}
	adapter.readyWait.Add(1)
	return adapter
}

func (s *WebTransportAdapter) Close() error {
	if s.stream != nil {
		s.stream.Close()
	}
	return s.conn.Close()
}

func (s *WebTransportAdapter) GetTLSConnectionState() (tls.ConnectionState, bool) {
	return s.qconn.ConnectionState().TLS.ConnectionState, true
}

func (s *WebTransportAdapter) Serve() (error, bool) {
	var err error

	if s.isServer {
		s.stream, err = s.conn.AcceptStream(context.Background())
	} else {
		s.stream, err = s.conn.OpenStreamSync(context.Background())
	}

	if err != nil {
		return err, true
	}

	s.streamId = getStreamID(s.stream)

	s.wg.Add(1)
	go s.serveControl()

	s.wg.Add(1)
	go s.serveData()

	s.isReady = true
	s.readyWait.Done()

	s.wg.Wait()

	return s.lastServeError, s.lastServeErrorUnexpected
}

func (s *WebTransportAdapter) handleServeError(err error, unexpected bool) {
	if s.lastServeError == nil {
		s.lastServeError = err
		s.lastServeErrorUnexpected = unexpected
	}
}

func (s *WebTransportAdapter) serveControl() {
	defer s.wg.Done()
	defer s.Close()

	var len uint64
	var msgType StreamMessageType
	var err error
	reader := bufio.NewReader(s.stream)

serveLoop:
	for {
		msgType, err = reader.ReadByte()
		if err != nil {
			break
		}

		switch msgType {
		case messageTypeControl:
			len, err = quicvarint.Read(reader)
			if err != nil {
				break
			}

			data := make([]byte, len)
			_, err = io.ReadFull(reader, data)
			if err != nil {
				break
			}

			s.controlMessageHandler(data)

		case messageTypePing:
			_, err = s.stream.Write([]byte{messageTypePong})
			if err != nil {
				break serveLoop
			}

		case messageTypePong:
			if s.pongHandler != nil {
				s.pongHandler()
			}
		}
	}

	if err != nil {
		s.handleServeError(err, err != io.EOF)
	}
}

func (s *WebTransportAdapter) serveData() {
	defer s.wg.Done()
	defer s.Close()

	for {
		data, err := s.qconn.ReceiveMessage()
		if err != nil {
			s.handleServeError(err, true)
			break
		}
		buf := bytes.NewBuffer(data)
		quarterStreamId, err := quicvarint.Read(buf)
		if err != nil {
			s.handleServeError(err, true)
			break
		}
		if quarterStreamId*4 != s.streamId {
			s.handleServeError(errors.New("wrong quarterStreamId"), true)
			break
		}
		s.dataMessageHandler(buf.Bytes())
	}
}

func (s *WebTransportAdapter) WaitReady() {
	s.readyWait.Wait()
}

func (s *WebTransportAdapter) WriteControlMessage(message []byte) error {
	if !s.isReady {
		return errors.New("not ready")
	}

	buf := &bytes.Buffer{}
	buf.WriteByte(messageTypeControl)
	quicvarint.Write(buf, uint64(len(message)))
	buf.Write(message)

	msg := buf.Bytes()

	n, err := s.stream.Write(msg)
	if err == nil && n != len(msg) {
		return errors.New("could not write full message")
	}

	return err
}

func (s *WebTransportAdapter) WriteDataMessage(message []byte) error {
	if !s.isReady {
		return errors.New("not ready")
	}

	buf := &bytes.Buffer{}
	quicvarint.Write(buf, s.streamId/4)
	buf.Write(message)
	return s.qconn.SendMessage(buf.Bytes())
}

func (s *WebTransportAdapter) WritePingMessage() error {
	if !s.isReady {
		return errors.New("not ready")
	}

	_, err := s.stream.Write([]byte{messageTypePing})
	return err
}

func (s *WebTransportAdapter) Name() string {
	return "WebTransport"
}
