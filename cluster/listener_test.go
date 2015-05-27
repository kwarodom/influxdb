package cluster_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/influxdb/influxdb/cluster"
	"github.com/influxdb/influxdb/meta"
	"github.com/influxdb/influxdb/tsdb"
)

type metaStore struct {
	host string
}

func (m *metaStore) Node(nodeID uint64) (*meta.NodeInfo, error) {
	return &meta.NodeInfo{
		ID:   nodeID,
		Host: m.host,
	}, nil
}

type testServer struct {
	writeShardFunc func(shardID uint64, points []tsdb.Point) error
}

func newTestServer(f func(shardID uint64, points []tsdb.Point) error) testServer {
	return testServer{
		writeShardFunc: f,
	}
}

type serverResponses []serverResponse
type serverResponse struct {
	shardID uint64
	points  []tsdb.Point
}

func (t testServer) WriteShard(shardID uint64, points []tsdb.Point) error {
	return t.writeShardFunc(shardID, points)
}

func writeShardSuccess(shardID uint64, points []tsdb.Point) error {
	responses <- &serverResponse{
		shardID: shardID,
		points:  points,
	}
	return nil
}

func writeShardFail(shardID uint64, points []tsdb.Point) error {
	return fmt.Errorf("failed to write")
}

var responses = make(chan *serverResponse, 1024)

func (testServer) ResponseN(n int) ([]*serverResponse, error) {
	var a []*serverResponse
	for {
		select {
		case r := <-responses:
			a = append(a, r)
			if len(a) == n {
				return a, nil
			}
		case <-time.After(time.Second):
			return a, fmt.Errorf("unexpected response count: expected: %d, actual: %d", n, len(a))
		}
	}
}

func TestServer_Close_ErrServerClosed(t *testing.T) {
	var (
		ts testServer
		s  = cluster.NewServer(ts, "127.0.0.1:0")
	)

	if e := s.Open(); e != nil {
		t.Fatalf("err does not match.  expected %v, got %v", nil, e)
	}

	// Close the server
	s.Close()

	// Try to close it again
	if err := s.Close(); err != cluster.ErrServerClosed {
		t.Fatalf("expected an error, got %v", err)
	}
}

func TestServer_Close_ErrBindAddressRequired(t *testing.T) {
	var (
		ts testServer
		s  = cluster.NewServer(ts, "")
	)
	if e := s.Open(); e == nil {
		t.Fatalf("exprected error %s, got nil.", cluster.ErrBindAddressRequired)
	}
}

func TestServer_WriteShardRequestSuccess(t *testing.T) {
	var (
		ts = newTestServer(writeShardSuccess)
		s  = cluster.NewServer(ts, "127.0.0.1:0")
	)
	e := s.Open()
	if e != nil {
		t.Fatalf("err does not match.  expected %v, got %v", nil, e)
	}
	// Close the server
	defer s.Close()

	writer := cluster.NewWriter(&metaStore{host: s.Addr().String()})

	now := time.Now()

	shardID := uint64(1)
	ownerID := uint64(2)
	var points []tsdb.Point
	points = append(points, tsdb.NewPoint(
		"cpu", tsdb.Tags{"host": "server01"}, map[string]interface{}{"value": int64(100)}, now,
	))

	if err := writer.Write(shardID, ownerID, points); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	responses, err := ts.ResponseN(1)
	if err != nil {
		t.Fatal(err)
	}

	response := responses[0]

	if shardID != response.shardID {
		t.Fatalf("unexpected shardID.  exp: %d, got %d", shardID, response.shardID)
	}

	got := response.points[0]
	exp := points[0]
	t.Log("got: ", spew.Sdump(got))
	t.Log("exp: ", spew.Sdump(exp))

	if got.Name() != exp.Name() {
		t.Fatal("unexpected name")
	}

	if got.Fields()["value"] != exp.Fields()["value"] {
		t.Fatal("unexpected fields")
	}

	if got.Tags()["host"] != exp.Tags()["host"] {
		t.Fatal("unexpected tags")
	}

	if got.Time().UnixNano() != exp.Time().UnixNano() {
		t.Fatal("unexpected time")
	}
}

func TestServer_WriteShardRequestMultipleSuccess(t *testing.T) {
	var (
		ts = newTestServer(writeShardSuccess)
		s  = cluster.NewServer(ts, "127.0.0.1:0")
	)
	// Start on a random port
	if e := s.Open(); e != nil {
		t.Fatalf("err does not match.  expected %v, got %v", nil, e)
	}
	// Close the server
	defer s.Close()

	writer := cluster.NewWriter(&metaStore{host: s.Addr().String()})

	now := time.Now()

	shardID := uint64(1)
	ownerID := uint64(2)
	var points []tsdb.Point
	points = append(points, tsdb.NewPoint(
		"cpu", tsdb.Tags{"host": "server01"}, map[string]interface{}{"value": int64(100)}, now,
	))

	if err := writer.Write(shardID, ownerID, points); err != nil {
		t.Fatal(err)
	}

	now = time.Now()

	points = append(points, tsdb.NewPoint(
		"cpu", tsdb.Tags{"host": "server01"}, map[string]interface{}{"value": int64(100)}, now,
	))

	if err := writer.Write(shardID, ownerID, points[1:]); err != nil {
		t.Fatal(err)
	}

	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	responses, err := ts.ResponseN(1)
	if err != nil {
		t.Fatal(err)
	}

	response := responses[0]

	if shardID != response.shardID {
		t.Fatalf("unexpected shardID.  exp: %d, got %d", shardID, response.shardID)
	}

	got := response.points[0]
	exp := points[0]
	t.Log("got: ", spew.Sdump(got))
	t.Log("exp: ", spew.Sdump(exp))

	if got.Name() != exp.Name() {
		t.Fatal("unexpected name")
	}

	if got.Fields()["value"] != exp.Fields()["value"] {
		t.Fatal("unexpected fields")
	}

	if got.Tags()["host"] != exp.Tags()["host"] {
		t.Fatal("unexpected tags")
	}

	if got.Time().UnixNano() != exp.Time().UnixNano() {
		t.Fatal("unexpected time")
	}
}

func TestServer_WriteShardRequestFail(t *testing.T) {
	var (
		ts = newTestServer(writeShardFail)
		s  = cluster.NewServer(ts, "127.0.0.1:0")
	)
	// Start on a random port
	if e := s.Open(); e != nil {
		t.Fatalf("err does not match.  expected %v, got %v", nil, e)
	}
	// Close the server
	defer s.Close()

	writer := cluster.NewWriter(&metaStore{host: s.Addr().String()})
	now := time.Now()

	shardID := uint64(1)
	ownerID := uint64(2)
	var points []tsdb.Point
	points = append(points, tsdb.NewPoint(
		"cpu", tsdb.Tags{"host": "server01"}, map[string]interface{}{"value": int64(100)}, now,
	))

	if err, exp := writer.Write(shardID, ownerID, points), "error code 1: failed to write"; err == nil || err.Error() != exp {
		t.Fatalf("expected error %s, got %v", exp, err)
	}
}