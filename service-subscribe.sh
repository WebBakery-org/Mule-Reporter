SOURCE_FOLDER=$(pwd)
chmod +x "${SOURCE_FOLDER}/mule-reporter"
sudo tee /etc/systemd/system/mule.service > /dev/null <<EOF
[Unit]
Description=WebBakery Mule Reporter Agent
After=network.target docker.service
Requires=docker.service

[Service]
Type=simple
User=root
Group=root
ExecStart=${SOURCE_FOLDER}/mule-reporter
WorkingDirectory=${SOURCE_FOLDER}/
Restart=always
RestartSec=5
MemoryMax=15M
CPUWeight=100
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload && systemctl enable --now mule
sudo systemctl restart mule

echo "Installed the Mule. Take care of her !"