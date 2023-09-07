# build the ./website binary
website : main.go
	go build -o bin/website  main.go 

# clear out bin folder
clean :
	-rm -rf bin/