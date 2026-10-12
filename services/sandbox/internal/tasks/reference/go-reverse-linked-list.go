//go:build ignore

package task

// Node — узел односвязного списка (задаётся в starter).
type Node struct {
	Value int
	Next  *Node
}

// Reverse разворачивает односвязный список на месте и возвращает новый голову.
func Reverse(head *Node) *Node {
	var prev *Node
	cur := head
	for cur != nil {
		next := cur.Next
		cur.Next = prev
		prev = cur
		cur = next
	}
	return prev
}
