// +build windows
package main

import (
	"fmt"
	"github.com/Doridian/wstun_shared"
	"github.com/songgao/water"
	"net"
)

func configIface(dev *water.Interface, rNet *remoteNet, mtu int, routeGateway bool) error {
	gw := "gateway=none"
	if routeGateway {
		gw = fmt.Sprintf("gateway=%s", rNet.getServerIP())
	}
	err := wstun_shared.ExecCmd("netsh", "interface", "ip", "set", "address", "source=static", fmt.Sprintf("addr=%s", rNet.getClientIP()), fmt.Sprintf("name=%s", dev.Name()), fmt.Sprintf("mask=%s", rNet.getNetmask()), gw)
	if err != nil {
		return err
	}
	return nil
}

func getPlatformSpecifics(rNet *remoteNet, mtu int, config water.Config) water.Config {
	config.ComponentID = "tap0901"
	config.Network = rNet.str
	return config
}

func addRoute(dev *water.Interface, rNet *remoteNet, routeNet *net.IPNet) error {
	return wstun_shared.ExecCmd("route", "ADD", routeNet.String(), rNet.getServerIP())
}
