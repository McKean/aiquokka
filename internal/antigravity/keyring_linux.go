//go:build linux

package antigravity

import (
	"context"
	"errors"
	"fmt"

	"github.com/godbus/dbus/v5"
)

const (
	ssServiceName  = "org.freedesktop.secrets"
	ssServicePath  = dbus.ObjectPath("/org/freedesktop/secrets")
	ssDefaultAlias = "default"

	ssIfaceService    = "org.freedesktop.Secret.Service"
	ssIfaceCollection = "org.freedesktop.Secret.Collection"
	ssIfaceItem       = "org.freedesktop.Secret.Item"
	ssIfaceSession    = "org.freedesktop.Secret.Session"
	dbusIfaceProps    = "org.freedesktop.DBus.Properties"
)

type ssSecret struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func readKeyringCredential(parent context.Context) ([]byte, error) {
	ctx, cancel := keyringContext(parent)
	defer cancel()

	conn, svc, sessionPath, err := openSecretSession(ctx)
	if err != nil {
		return nil, err
	}
	defer closeSecretSession(conn, sessionPath)

	attrs := map[string]string{
		"service":  keyringService,
		"username": keyringAccount,
	}
	var unlocked, locked []dbus.ObjectPath
	if err := svc.CallWithContext(ctx, ssIfaceService+".SearchItems", 0, attrs).Store(&unlocked, &locked); err != nil {
		return nil, classifyDBusError(ctx, err)
	}
	if len(unlocked) == 0 {
		if len(locked) > 0 {
			return nil, errKeyringLocked
		}
		return nil, errKeyringNotFound
	}

	var sec ssSecret
	item := conn.Object(ssServiceName, unlocked[0])
	if err := item.CallWithContext(ctx, ssIfaceItem+".GetSecret", 0, sessionPath).Store(&sec); err != nil {
		return nil, classifyDBusError(ctx, err)
	}
	out := decodeKeyringSecret(sec.Value)
	if len(out) == 0 {
		return nil, errKeyringNotFound
	}
	return out, nil
}

func writeKeyringCredential(parent context.Context, data []byte) error {
	ctx, cancel := keyringContext(parent)
	defer cancel()

	conn, svc, sessionPath, err := openSecretSession(ctx)
	if err != nil {
		return err
	}
	defer closeSecretSession(conn, sessionPath)

	var colPath dbus.ObjectPath
	if err := svc.CallWithContext(ctx, ssIfaceService+".ReadAlias", 0, ssDefaultAlias).Store(&colPath); err != nil {
		return classifyDBusError(ctx, err)
	}
	if colPath == "" || colPath == "/" {
		colPath = dbus.ObjectPath("/org/freedesktop/secrets/aliases/default")
	}

	var locked dbus.Variant
	col := conn.Object(ssServiceName, colPath)
	if err := col.CallWithContext(ctx, dbusIfaceProps+".Get", 0, ssIfaceCollection, "Locked").Store(&locked); err == nil {
		if isLocked, _ := locked.Value().(bool); isLocked {
			return errKeyringLocked
		}
	}

	props := map[string]dbus.Variant{
		ssIfaceItem + ".Label": dbus.MakeVariant(fmt.Sprintf("Password for '%s' on '%s'", keyringAccount, keyringService)),
		ssIfaceItem + ".Attributes": dbus.MakeVariant(map[string]string{
			"service":  keyringService,
			"username": keyringAccount,
		}),
	}
	sec := ssSecret{
		Session:     sessionPath,
		Parameters:  []byte{},
		Value:       data,
		ContentType: "text/plain",
	}
	var created, prompt dbus.ObjectPath
	if err := col.CallWithContext(ctx, ssIfaceCollection+".CreateItem", 0, props, sec, true).Store(&created, &prompt); err != nil {
		return classifyDBusError(ctx, err)
	}
	if prompt != "" && prompt != "/" {
		return errKeyringLocked
	}
	return nil
}

func keyringContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, keyringTimeout)
}

func openSecretSession(ctx context.Context) (*dbus.Conn, dbus.BusObject, dbus.ObjectPath, error) {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, "", classifyDBusError(ctx, err)
		}
		return nil, nil, "", errKeyringNotFound
	}
	svc := conn.Object(ssServiceName, ssServicePath)
	var ignored dbus.Variant
	var sessionPath dbus.ObjectPath
	if err := svc.CallWithContext(ctx, ssIfaceService+".OpenSession", 0, "plain", dbus.MakeVariant("")).Store(&ignored, &sessionPath); err != nil {
		_ = conn.Close()
		return nil, nil, "", classifyDBusError(ctx, err)
	}
	return conn, svc, sessionPath, nil
}

func closeSecretSession(conn *dbus.Conn, sessionPath dbus.ObjectPath) {
	if sessionPath != "" && sessionPath != "/" {
		_ = conn.Object(ssServiceName, sessionPath).Call(ssIfaceSession+".Close", 0).Err
	}
	_ = conn.Close()
}

func classifyDBusError(ctx context.Context, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	if ctx.Err() != nil {
		return errKeyringTimeout
	}
	var de dbus.Error
	if errors.As(err, &de) {
		switch de.Name {
		case "org.freedesktop.DBus.Error.ServiceUnknown",
			"org.freedesktop.DBus.Error.NameHasNoOwner",
			"org.freedesktop.Secret.Error.NoSuchObject":
			return errKeyringNotFound
		case "org.freedesktop.Secret.Error.IsLocked":
			return errKeyringLocked
		}
	}
	return err
}
