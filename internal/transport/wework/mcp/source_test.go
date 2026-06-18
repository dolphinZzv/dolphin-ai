package mcp

import (
	"context"
	"fmt"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	transport "dolphin/internal/transport"
	"dolphin/internal/types"
)

// mockClient implements WeWorkClient for testing.
type mockClient struct {
	proactiveMsg func(ctx context.Context, content, msgType string) error
	uploadMedia  func(ctx context.Context, filePath string) (string, string, string, error)
	sendMedia    func(ctx context.Context, mediaID, mediaType string) error
}

func (m *mockClient) ProactiveMessage(ctx context.Context, content, msgType string) error {
	if m.proactiveMsg != nil {
		return m.proactiveMsg(ctx, content, msgType)
	}
	return nil
}

func (m *mockClient) UploadMedia(ctx context.Context, filePath string) (string, string, string, error) {
	if m.uploadMedia != nil {
		return m.uploadMedia(ctx, filePath)
	}
	return "mock_media_id", "test.png", "image", nil
}

func (m *mockClient) SendMediaMessage(ctx context.Context, mediaID, mediaType string) error {
	if m.sendMedia != nil {
		return m.sendMedia(ctx, mediaID, mediaType)
	}
	return nil
}

func TestWeWorkMCPSource(t *testing.T) {
	Convey("WeWork MCP source", t, func() {
		src := NewSource(&mockClient{}, "test_bot", "test_secret", nil)
		wCtx := transport.WithInfo(context.Background(), &transport.Info{ID: "wework"})

		Convey("List returns tools only with correct transport context", func() {
			Convey("with wework context returns FILE_UPLOAD and MESSAGE", func() {
				defs, err := src.List(wCtx)
				So(err, ShouldBeNil)
				So(defs, ShouldHaveLength, 2)
				So(defs[0].Name, ShouldEqual, "FILE_UPLOAD")
				So(defs[1].Name, ShouldEqual, "MESSAGE")
			})

			Convey("without context returns nil", func() {
				defs, err := src.List(context.Background())
				So(err, ShouldBeNil)
				So(defs, ShouldBeNil)
			})

			Convey("with wrong transport ID returns nil", func() {
				ctx := transport.WithInfo(context.Background(), &transport.Info{ID: "stdio"})
				defs, err := src.List(ctx)
				So(err, ShouldBeNil)
				So(defs, ShouldBeNil)
			})
		})

		Convey("Execute with unknown tool returns error", func() {
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name: "UNKNOWN",
			})
			So(err, ShouldNotBeNil)
			So(result, ShouldBeNil)
		})

		Convey("Execute MESSAGE with empty content returns error", func() {
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "MESSAGE",
				Arguments: `{"content": ""}`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "content is required")
		})

		Convey("Execute MESSAGE with invalid JSON returns error", func() {
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "MESSAGE",
				Arguments: `not json`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "invalid arguments")
		})

		Convey("Execute FILE_UPLOAD with invalid JSON returns error", func() {
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "FILE_UPLOAD",
				Arguments: `not json`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "invalid arguments")
		})

		Convey("Execute FILE_UPLOAD with UploadMedia error", func() {
			src := NewSource(&mockClient{
				uploadMedia: func(ctx context.Context, filePath string) (string, string, string, error) {
					return "", "", "", fmt.Errorf("upload failed")
				},
			}, "test_bot", "test_secret", nil)
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "FILE_UPLOAD",
				Arguments: `{"file_path": "/tmp/test.txt"}`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "upload failed")
		})

		Convey("Execute FILE_UPLOAD with SendMediaMessage error", func() {
			src := NewSource(&mockClient{
				sendMedia: func(ctx context.Context, mediaID, mediaType string) error {
					return fmt.Errorf("send failed")
				},
			}, "test_bot", "test_secret", nil)
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "FILE_UPLOAD",
				Arguments: `{"file_path": "/tmp/test.txt"}`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "send failed")
		})

		Convey("Execute FILE_UPLOAD success", func() {
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "FILE_UPLOAD",
				Arguments: `{"file_path": "/tmp/test.txt"}`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeFalse)
			So(result.Content, ShouldContainSubstring, "mock_media_id")
			So(result.Content, ShouldContainSubstring, "test.png")
		})

		Convey("Execute MESSAGE with ProactiveMessage error", func() {
			src := NewSource(&mockClient{
				proactiveMsg: func(ctx context.Context, content, msgType string) error {
					return fmt.Errorf("send failed")
				},
			}, "test_bot", "test_secret", nil)
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "MESSAGE",
				Arguments: `{"content": "hello"}`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeTrue)
			So(result.Content, ShouldContainSubstring, "send failed")
		})

		Convey("Execute MESSAGE success with default msgtype", func() {
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "MESSAGE",
				Arguments: `{"content": "hello"}`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeFalse)
			So(result.Content, ShouldContainSubstring, "sent successfully")
		})

		Convey("Execute MESSAGE success with explicit msgtype", func() {
			result, err := src.Execute(context.Background(), types.ToolCall{
				Name:      "MESSAGE",
				Arguments: `{"content": "hello", "msgtype": "text"}`,
			})
			So(err, ShouldBeNil)
			So(result.IsError, ShouldBeFalse)
			So(result.Content, ShouldContainSubstring, "sent successfully")
		})
	})
}
