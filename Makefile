.PHONY: clean build

build: influence-eth

clean:
	rm -f ./influence-eth

rebuild: clean build

influence-eth:
	go build cmd/influence/*.go
