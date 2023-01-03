# Serial Logger

This application was developed for a project involving custom hardware.
In order to debug our hardware at the customer's site, a Raspberry Pi accesses the serial port and logs the information.

Via RaspAP an access point can be opened to access the hardware without entering the customer's premises. Alternatively, a simple LTE router can be connected to ensure access via tunnel or VPN.

Offline use has the disadvantage that no current time is available. However, since the hardware synchronizes its time via HTTP, these lines are parsed and the time offset is calculated.

```
# 2006-01-02 15:04:05
```

The software is written in such a way that several TTY interfaces can be connected. These can also be connected or disconnected during operation. This is handled by the software.