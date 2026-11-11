package main

import (
	"bytes"
	"io"
	"net/http"
)

type recorder struct {
	h    http.Header
	code int
	body bytes.Buffer
}

func newRecorder() *recorder { return &recorder{h: http.Header{}, code: 200} }

func (r *recorder) Header() http.Header         { return r.h }
func (r *recorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *recorder) WriteHeader(c int)           { r.code = c }

func (r *recorder) result(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: r.code, Status: http.StatusText(r.code), Header: r.h, Request: req,
		Body: io.NopCloser(bytes.NewReader(r.body.Bytes())), ContentLength: int64(r.body.Len()),
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
	}
}
