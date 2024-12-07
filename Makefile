.PHONY: all build clean

all: build

build:
	go build -o prompt_generator main.go

clean:
	rm -f prompt_generator
