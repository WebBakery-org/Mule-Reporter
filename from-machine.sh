# Gets the mule from another machine and run it on this machine.
MACHINE=$1
PORT=$2

sudo apt install jq

if [ -z "$MACHINE" ] || [ -z "$PORT" ]; then
	echo "Usage: $0 <ip> <port>"
	exit 1
fi

echo "Connecting to the Mule on $MACHINE:$PORT..."
echo getme | nc "$MACHINE" "$PORT" > response.json

if [ ! -s response.json ]; then
    echo "Error: Received empty response."
    exit 1
fi

echo "Retrieved content from the mule."
jq -r '.message.installation_script' 	response.json 					> install-mule.sh
jq -r '.message.config' 				response.json 					> conf.ini
jq -r '.message.base64_binary' 			response.json | base64 --decode	> mule-reporter
chmod +x ./install-mule.sh ./mule-reporter
rm response.json
echo "The Mule is ready to be launched."
