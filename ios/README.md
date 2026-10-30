# Sonar DMX for iPad

A standalone iPad app: the iPad runs the controller and sends the Art-Net
itself, no Mac needed at the show. It is the same controller as the Mac app
(same Perform layouts, programs, music mode, patch, fixture profiles and
channel sweep), with its own saved patch.

## Install it (once, from the Mac)

Apple only lets apps outside the App Store onto an iPad through Xcode or a
developer account, so the first install goes through the Mac.

1. Install **Xcode** from the Mac App Store (free) and open it once.
2. Unzip `Sonar_DMX_iPad_v0.3` and double-click `SonarDMX.xcodeproj`.
3. In Xcode, click **SonarDMX** at the top of the left sidebar, then the
   **Signing & Capabilities** tab. Under Team, choose **Add an Account…** and
   sign in with your Apple ID, then pick "(Personal Team)". If Xcode says the
   bundle identifier is not available, change `com.dvnley.sonardmx` to
   something unique, such as `com.dvnley.sonardmx2`.
4. Plug the iPad into the Mac with a cable, unlock it and tap **Trust**.
5. On the iPad: Settings, Privacy & Security, **Developer Mode**, turn it on
   and let the iPad restart.
6. In Xcode's toolbar, pick the iPad as the run destination and press the
   ▶ Run button.
7. The first time, the iPad says "Untrusted Developer": open Settings,
   General, **VPN & Device Management**, tap your Apple ID and **Trust**.
   Then open Sonar DMX from the home screen.

With a free Apple ID the app stops opening after **7 days**. Plug the iPad in
and press Run in Xcode again; your patch and scenes are kept. A paid Apple
Developer account ($99/year) makes it last a year and allows TestFlight.

The kit also has `Sonar DMX unsigned.ipa` for sideloading tools such as
Sideloadly or AltStore, which sign it with your Apple ID instead of Xcode.

## Connect the DMX-AN2

The iPad needs a network path to the AN2, either way works:

- **Wired (recommended):** a USB-C Ethernet adapter (or Apple's Lightning to
  USB 3 Camera Adapter plus a USB Ethernet adapter on older iPads). Plug the
  AN2 into it, then on the iPad: Settings, **Ethernet**, tap the adapter,
  Configure IP: **Manual**, IP `2.0.0.2`, Subnet Mask `255.0.0.0`, no router.
  Wi-Fi keeps working alongside.
- **Wi-Fi:** plug the AN2 into your Wi-Fi router, give the AN2 an address on
  that network, and type that address as the target IP in the app's Setup
  tab.

When iOS asks to let Sonar DMX find devices on your local network, tap
**Allow** (if you missed it: Settings, Privacy & Security, Local Network).
The status pill turns green ("AN2 online") once the AN2 answers.

Only one Art-Net sender at a time: quit the Mac app while the iPad runs the
show.

## Good to know

- Keep the app on screen during a show. It stops the iPad from dimming or
  locking, but iOS pauses any app you switch away from, which stops the
  Art-Net until you come back.
- The patch starts as the default rig. Set your real addresses in the Patch
  tab (wash at 1, spots at 17 if you use the Monoprice layout).
- Music mode's mic listening works on the iPad (it asks for the mic once).

## Building from source

`ios/build-framework.sh` builds `Sonar.xcframework` from the Go code (needs
Go and Xcode). CI builds it and the whole kit on every push; download the
`Sonar_DMX_iPad_v0.3` artifact from the GitHub Actions run.
