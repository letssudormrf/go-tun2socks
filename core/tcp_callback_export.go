package core

/*
#cgo CFLAGS: -I./src/include
#include "lwip/tcp.h"
*/
import "C"
import (
	"errors"
	"fmt"
	"log"
	"unsafe"
)

// These exported callback functions must be placed in a seperated file.
//
// See also:
// https://github.com/golang/go/issues/20639
// https://golang.org/cmd/cgo/#hdr-C_references_to_Go

//export TCPAcceptFn
func TCPAcceptFn(arg unsafe.Pointer, newpcb *C.struct_tcp_pcb, err C.err_t) C.err_t {
	if err != C.ERR_OK {
		return err
	}

	conn, err2 := NewTCPConnection(newpcb, tcpConnectionHandler)
	if err2 != nil {
		log.Printf("failed to create TCP connection %v:%v->%v:%v: %v", GetIPAddr(newpcb.local_ip), uint16(newpcb.local_port), GetIPAddr(newpcb.remote_ip), uint16(newpcb.remote_port), err2)
		return C.ERR_OK
	}

	log.Printf("created new TCP connection %v->%v", conn.LocalAddr(), conn.RemoteAddr())

	return C.ERR_OK
}

//export TCPRecvFn
func TCPRecvFn(arg unsafe.Pointer, tpcb *C.struct_tcp_pcb, p *C.struct_pbuf, err C.err_t) C.err_t {
	if err != C.ERR_OK && err != C.ERR_ABRT {
		log.Printf("receving data failed with lwip error code: %v", int(err))
		return err
	}

	// Only free the pbuf when returning ERR_OK or ERR_ABRT.
	defer func() {
		if p != nil {
			C.pbuf_free(p)
		}
	}()

	conn, ok := tcpConns.Load(GetConnKeyVal(arg))
	if !ok {
		// The connection does not exists.
		log.Printf("connection does not exists")
		C.tcp_abort(tpcb)
		return C.ERR_ABRT
	}

	if p == nil {
		// The connection has been closed.
		err := conn.(Connection).LocalDidClose()
		if err.(*lwipError).Code == LWIP_ERR_ABRT {
			return C.ERR_ABRT
		} else if err.(*lwipError).Code == LWIP_ERR_OK {
			return C.ERR_OK
		}
	}

	if tpcb == nil {
		log.Fatal("tcp_recv pcb is nil")
	}

	// TODO: p.tot_len != p.len, have multiple pbuf in the chain?
	if p.tot_len != p.len {
		log.Fatal("tcp_recv p.tot_len != p.len (%v != %v)", p.tot_len, p.len)
	}

	// create Go slice backed by C array, the slice will not garbage collect by Go runtime
	buf := (*[1 << 30]byte)(unsafe.Pointer(p.payload))[:int(p.tot_len):int(p.tot_len)]
	handlerErr := conn.(Connection).Receive(buf)

	if handlerErr != nil {
		log.Printf("receive data failed: %v", handlerErr)
		C.tcp_abort(tpcb)
		return C.ERR_ABRT
	}

	return C.ERR_OK
}

//export TCPSentFn
func TCPSentFn(arg unsafe.Pointer, tpcb *C.struct_tcp_pcb, len C.u16_t) C.err_t {
	if conn, ok := tcpConns.Load(GetConnKeyVal(arg)); ok {
		err := conn.(Connection).Sent(uint16(len))
		if err.(*lwipError).Code == LWIP_ERR_ABRT {
			return C.ERR_ABRT
		} else {
			return C.ERR_OK
		}
	} else {
		log.Printf("connection does not exists")
		C.tcp_abort(tpcb)
		return C.ERR_ABRT
	}
}

//export TCPErrFn
func TCPErrFn(arg unsafe.Pointer, err C.err_t) {
	if conn, ok := tcpConns.Load(GetConnKeyVal(arg)); ok {
		switch err {
		case C.ERR_ABRT:
			// Aborted through tcp_abort or by a TCP timer
			conn.(Connection).Err(errors.New("connection aborted"))
		case C.ERR_RST:
			// The connection was reset by the remote host
			conn.(Connection).Err(errors.New("connection reseted"))
		default:
			conn.(Connection).Err(errors.New(fmt.Sprintf("lwip error code %v", int(err))))
		}
	}
}

//export TCPPollFn
func TCPPollFn(arg unsafe.Pointer, tpcb *C.struct_tcp_pcb) C.err_t {
	if conn, ok := tcpConns.Load(GetConnKeyVal(arg)); ok {
		err := conn.(Connection).Poll()
		if err.(*lwipError).Code == LWIP_ERR_ABRT {
			return C.ERR_ABRT
		} else if err.(*lwipError).Code == LWIP_ERR_OK {
			return C.ERR_OK
		}
	}
	return C.ERR_OK
}
