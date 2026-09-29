package voipinfra

import (
	"errors"
	"fmt"
	"net"
	"reflect"
	"unsafe"

	"github.com/emiago/diago/media"
)

var errMediaSessionNotInitialised = errors.New("media session has no RTP connection")

func sessionField(session *media.MediaSession, name string) (reflect.Value, error) {
	if session == nil {
		return reflect.Value{}, errMediaSessionNotInitialised
	}
	field := reflect.ValueOf(session).Elem().FieldByName(name)
	if !field.IsValid() {
		return reflect.Value{}, fmt.Errorf("diago media session has no %q field; the vendored diago version changed", name)
	}
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem(), nil
}

func packetConnValue(conn net.PacketConn) reflect.Value {
	value := reflect.New(reflect.TypeFor[net.PacketConn]()).Elem()
	value.Set(reflect.ValueOf(conn))
	return value
}

func sessionRTPConn(session *media.MediaSession) (net.PacketConn, error) {
	field, err := sessionField(session, "rtpConn")
	if err != nil {
		return nil, err
	}
	if field.IsNil() {
		return nil, errMediaSessionNotInitialised
	}
	return field.Interface().(net.PacketConn), nil
}

func sessionEncrypted(session *media.MediaSession) (bool, error) {
	local, err := sessionField(session, "localCtxSRTP")
	if err != nil {
		return false, err
	}
	remote, err := sessionField(session, "remoteCtxSRTP")
	if err != nil {
		return false, err
	}
	return !local.IsNil() && !remote.IsNil(), nil
}
