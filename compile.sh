SOURCE_FOLDER=$(pwd)
cd "${SOURCE_FOLDER}/go-source"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildvcs=false -ldflags="-s -w" -o "${SOURCE_FOLDER}/mule-reporter" .
sudo systemctl daemon-reload && systemctl enable --now mule
sudo systemctl restart mule || true

