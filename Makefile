default:
	go install -v ./go/userve
	go install -v ./go/ufparse

testgo:
	go test -v ./...
