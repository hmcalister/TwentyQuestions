dev: 
	air -tmp_dir "airtmp" --build.cmd "go build -o airtmp/main" --build.bin "airtmp/main" --build.send_interrupt True
	
build:
	go build -o bin . 