package garmin

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"strings"
)

// Port of the course-service calls courses.py makes directly through connectapi.

const coursePath = "/course-service/course"

func (c *Client) GetCourses(ctx context.Context) (any, error) { return c.GetJSON(ctx, coursePath, nil) }

func (c *Client) GetCourse(ctx context.Context, id int64) (any, error) {
	return c.GetJSON(ctx, coursePath+"/"+pathSeg(strconv.FormatInt(id, 10)), nil)
}

// ImportCourseGPX is step 1 of upload_course: the server parses the GPX into a course skeleton.
func (c *Client) ImportCourseGPX(ctx context.Context, filename string, gpx []byte) (any, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="`+quoteFilename(filename)+`"`)
	h.Set("Content-Type", "application/gpx+xml")
	fw, err := mw.CreatePart(h)
	if err != nil {
		return nil, err
	}
	fw.Write(gpx)
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: coursePath + "/import", Body: buf.Bytes(), ContentType: mw.FormDataContentType()})
}

// SaveCourse is step 2 of upload_course.
func (c *Client) SaveCourse(ctx context.Context, payload any) (any, error) {
	return c.DoJSON(ctx, Req{Method: http.MethodPost, Path: coursePath, JSON: payload})
}

func (c *Client) DeleteCourse(ctx context.Context, id int64) error {
	_, err := c.Do(ctx, Req{Method: http.MethodDelete, Path: coursePath + "/" + pathSeg(strconv.FormatInt(id, 10))})
	return err
}

var filenameEscaper = []string{`\`, `\\`, `"`, `\"`}

func quoteFilename(s string) string {
	for i := 0; i < len(filenameEscaper); i += 2 {
		s = strings.ReplaceAll(s, filenameEscaper[i], filenameEscaper[i+1])
	}
	return s
}
