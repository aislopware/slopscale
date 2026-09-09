package db

import (
	"strconv"
	"testing"
	"time"

	"github.com/aislopware/slopscale/hscontrol/types"
)

const benchNodeCount = 100

// benchDB opens a file-backed SQLite database exactly as the server does
// and fills it with benchNodeCount nodes of one user.
func benchDB(b *testing.B) (*HSDatabase, types.Nodes) {
	b.Helper()

	db, err := newSQLiteTestDB()
	if err != nil {
		b.Fatal(err)
	}

	b.Cleanup(func() { _ = db.Close() })

	user, err := db.CreateUser(types.User{Name: "bench"})
	if err != nil {
		b.Fatal(err)
	}

	for i := range benchNodeCount {
		db.CreateNodeForTest(user, "node-"+strconv.Itoa(i))
	}

	nodes, err := db.ListNodes()
	if err != nil {
		b.Fatal(err)
	}

	return db, nodes
}

func BenchmarkGetNodeByID(b *testing.B) {
	db, nodes := benchDB(b)

	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		_, err := db.GetNodeByID(nodes[i%len(nodes)].ID)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListNodes(b *testing.B) {
	db, _ := benchDB(b)

	b.ResetTimer()

	for b.Loop() {
		_, err := db.ListNodes()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkListPeers(b *testing.B) {
	db, nodes := benchDB(b)

	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		_, err := Read(db, func(rx *Tx) (types.Nodes, error) {
			return ListPeers(rx, nodes[i%len(nodes)].ID)
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSetLastSeen(b *testing.B) {
	db, nodes := benchDB(b)
	now := time.Now()

	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		err := db.SetLastSeen(nodes[i%len(nodes)].ID, now)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkUpdateNode is the map request persistence path: every Hostinfo
// or endpoint change lands here.
func BenchmarkUpdateNode(b *testing.B) {
	db, nodes := benchDB(b)

	b.ResetTimer()

	for i := 0; b.Loop(); i++ {
		node := nodes[i%len(nodes)]

		err := db.Write(func(tx *Tx) error {
			return UpdateNode(tx, node, NodeUpdate{})
		})
		if err != nil {
			b.Fatal(err)
		}
	}
}
