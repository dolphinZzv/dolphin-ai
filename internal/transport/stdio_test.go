package transport

import (
	"context"
	"io"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestStdioGetters(t *testing.T) {
	Convey("Stdio getters", t, func() {
		s := NewStdio("testuser")

		Convey("ID returns stdio", func() {
			So(s.ID(), ShouldEqual, "stdio")
		})

		Convey("Context returns empty", func() {
			So(s.Context(), ShouldEqual, "")
		})

		Convey("Tools returns nil", func() {
			So(s.Tools(), ShouldBeNil)
		})

		Convey("Start returns nil", func() {
			err := s.Start(context.Background())
			So(err, ShouldBeNil)
		})
	})
}

func TestNullTransportGetters(t *testing.T) {
	Convey("NullTransport getters", t, func() {
		n := NewNullTransport("test")

		Convey("ID returns given id", func() {
			So(n.ID(), ShouldEqual, "test")
		})

		Convey("Context returns empty", func() {
			So(n.Context(), ShouldEqual, "")
		})

		Convey("Tools returns nil", func() {
			So(n.Tools(), ShouldBeNil)
		})

		Convey("Start returns nil", func() {
			err := n.Start(context.Background())
			So(err, ShouldBeNil)
		})
	})
}

func TestNullTransportIO(t *testing.T) {
	Convey("NullTransport IO operations", t, func() {
		n := NewNullTransport("test")

		Convey("Write returns nil", func() {
			err := n.Write(context.Background(), "hello")
			So(err, ShouldBeNil)
		})

		Convey("Flush returns nil", func() {
			err := n.Flush()
			So(err, ShouldBeNil)
		})

		Convey("Read returns EOF on empty", func() {
			_, err := n.Read(context.Background())
			So(err, ShouldEqual, io.EOF)
		})

		Convey("RequestPermission returns PermissionDenied", func() {
			result, err := n.RequestPermission(context.Background(), "test")
			So(err, ShouldBeNil)
			So(result, ShouldEqual, PermissionDenied)
		})
	})
}
