# Local patches to fyne.io/systray v1.12.2

Upstream: https://github.com/fyne-io/systray (Apache-2.0, see LICENSE).

1. `systray_darwin.m` `setMenuItemIcon`: menu item images keep their own size
   (pixels / 2, i.e. @2x) instead of being forced to 16x16 pt. A 32x32 px icon
   still renders at 16x16 pt; wider images (aiquokka's tabular usage rows)
   render at full width.
