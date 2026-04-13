# BLE commissioning — network credentials

When onboarding (commissioning) a Matter device over BLE, the controller must
supply network credentials so the device can join the IP network.

## WiFi credentials for the local test network

| Field    | Value                |
|----------|----------------------|
| SSID     | `tomkat-iot`         |
| Password | `soviets-ferry-dork` |

Use these credentials whenever a BLE commissioning flow requires WiFi network
information (e.g. the `NetworkCommissioning` cluster `AddOrUpdateWiFiNetwork`
command).