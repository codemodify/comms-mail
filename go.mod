module github.com/codemodify/comms-mail

go 1.27

require (
	github.com/ProtonMail/go-crypto v1.5.2
	github.com/go-sql-driver/mysql v1.10.1
	github.com/godbus/dbus/v5 v5.1.0
	goframework.io/desktop-ui v1.0.0
	goframework.io/desktop-ui-icons v0.0.0-00010101000000-000000000000
	goframework.io/desktop-ui-platform-all v0.0.0-00010101000000-000000000000
	goframework.io/desktop-ui-richtext v0.0.0-00010101000000-000000000000
	goframework.io/desktop-ui-text-complex v0.0.0-00010101000000-000000000000
	goframework.io/desktop-ui-themes-all v0.0.0-00010101000000-000000000000
	goframework.io/desktop-ui-widgets v1.0.0
	goframework.io/graphics-paint2d v1.0.0
	golang.org/x/crypto v0.57.0
	golang.org/x/image v0.23.0
	golang.org/x/net v0.59.0
	golang.org/x/text v0.42.0
	modernc.org/sqlite v1.59.0
	software.sslmate.com/src/go-pkcs12 v0.7.3
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.24 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	goframework.io/codecs-font-opentype v1.0.0 // indirect
	goframework.io/desktop-ui-a11y v1.0.0 // indirect
	goframework.io/desktop-ui-platform-appkit v1.0.0 // indirect
	goframework.io/desktop-ui-platform-freedesktop v1.0.0 // indirect
	goframework.io/desktop-ui-platform-wayland v1.0.0 // indirect
	goframework.io/desktop-ui-platform-win32 v1.0.0 // indirect
	goframework.io/desktop-ui-platform-x11 v1.0.0 // indirect
	goframework.io/desktop-ui-themes-adwaita v1.0.0 // indirect
	goframework.io/desktop-ui-themes-aero v1.0.0 // indirect
	goframework.io/desktop-ui-themes-amiga v1.0.0 // indirect
	goframework.io/desktop-ui-themes-aqua v1.0.0 // indirect
	goframework.io/desktop-ui-themes-beos v1.0.0 // indirect
	goframework.io/desktop-ui-themes-bluecurve v1.0.0 // indirect
	goframework.io/desktop-ui-themes-breeze v1.0.0 // indirect
	goframework.io/desktop-ui-themes-clearlooks v1.0.0 // indirect
	goframework.io/desktop-ui-themes-flatlaf v1.0.0 // indirect
	goframework.io/desktop-ui-themes-fluent v1.0.0 // indirect
	goframework.io/desktop-ui-themes-fusion v1.0.0 // indirect
	goframework.io/desktop-ui-themes-hangar v1.0.0 // indirect
	goframework.io/desktop-ui-themes-kde1 v1.0.0 // indirect
	goframework.io/desktop-ui-themes-kde2 v1.0.0 // indirect
	goframework.io/desktop-ui-themes-keramik v1.0.0 // indirect
	goframework.io/desktop-ui-themes-luna v1.0.0 // indirect
	goframework.io/desktop-ui-themes-macos v1.0.0 // indirect
	goframework.io/desktop-ui-themes-material v1.0.0 // indirect
	goframework.io/desktop-ui-themes-metal v1.0.0 // indirect
	goframework.io/desktop-ui-themes-metro v1.0.0 // indirect
	goframework.io/desktop-ui-themes-motif v1.0.0 // indirect
	goframework.io/desktop-ui-themes-next v1.0.0 // indirect
	goframework.io/desktop-ui-themes-nimbus v1.0.0 // indirect
	goframework.io/desktop-ui-themes-openlook v1.0.0 // indirect
	goframework.io/desktop-ui-themes-os2 v1.0.0 // indirect
	goframework.io/desktop-ui-themes-oxygen v1.0.0 // indirect
	goframework.io/desktop-ui-themes-plastik v1.0.0 // indirect
	goframework.io/desktop-ui-themes-platinum v1.0.0 // indirect
	goframework.io/desktop-ui-themes-system7 v1.0.0 // indirect
	goframework.io/desktop-ui-themes-web v1.0.0 // indirect
	goframework.io/desktop-ui-themes-win31 v1.0.0 // indirect
	goframework.io/desktop-ui-themes-win95 v1.0.0 // indirect
	goframework.io/graphics-paint2d-gpu-gles v1.0.0 // indirect
	goframework.io/system-dbus v1.0.0 // indirect
	goframework.io/system-native-windows v0.0.3 // indirect
	golang.org/x/sys v0.48.0 // indirect
	modernc.org/libc v1.75.7 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.12.1 // indirect
)

// goframework.io is not published yet: every module comes from the local
// checkout of its workspace (make replace writes these lines).
replace (
	goframework.io/app => ../../../goframework.io/app
	goframework.io/app-as-service => ../../../goframework.io/app-as-service
	goframework.io/app-as-service-w-plugins => ../../../goframework.io/app-as-service-w-plugins
	goframework.io/app-plugin => ../../../goframework.io/app-plugin
	goframework.io/app-samples => ../../../goframework.io/app-samples
	goframework.io/app-spec => ../../../goframework.io/app-spec
	goframework.io/app-w-plugins => ../../../goframework.io/app-w-plugins
	goframework.io/callstack => ../../../goframework.io/callstack
	goframework.io/cliflags => ../../../goframework.io/cliflags
	goframework.io/codecs-compress-bcj => ../../../goframework.io/codecs-compress-bcj
	goframework.io/codecs-compress-delta => ../../../goframework.io/codecs-compress-delta
	goframework.io/codecs-compress-lzma => ../../../goframework.io/codecs-compress-lzma
	goframework.io/codecs-compress-lznt1 => ../../../goframework.io/codecs-compress-lznt1
	goframework.io/codecs-compress-ppmd => ../../../goframework.io/codecs-compress-ppmd
	goframework.io/codecs-compress-rar => ../../../goframework.io/codecs-compress-rar
	goframework.io/codecs-data-protobuf => ../../../goframework.io/codecs-data-protobuf
	goframework.io/codecs-font-opentype => ../../../goframework.io/codecs-font-opentype
	goframework.io/codecs-text-charsets => ../../../goframework.io/codecs-text-charsets
	goframework.io/codecs-text-patch => ../../../goframework.io/codecs-text-patch
	goframework.io/config-all => ../../../goframework.io/config-all
	goframework.io/config-cli => ../../../goframework.io/config-cli
	goframework.io/config-dotenv => ../../../goframework.io/config-dotenv
	goframework.io/config-hcl => ../../../goframework.io/config-hcl
	goframework.io/config-ini => ../../../goframework.io/config-ini
	goframework.io/config-json => ../../../goframework.io/config-json
	goframework.io/config-jsonc => ../../../goframework.io/config-jsonc
	goframework.io/config-properties => ../../../goframework.io/config-properties
	goframework.io/config-source-file => ../../../goframework.io/config-source-file
	goframework.io/config-spec => ../../../goframework.io/config-spec
	goframework.io/config-toml => ../../../goframework.io/config-toml
	goframework.io/config-xml => ../../../goframework.io/config-xml
	goframework.io/config-yaml => ../../../goframework.io/config-yaml
	goframework.io/cryptography-aead-aes-ccm => ../../../goframework.io/cryptography-aead-aes-ccm
	goframework.io/cryptography-cipher-aes-cbc => ../../../goframework.io/cryptography-cipher-aes-cbc
	goframework.io/cryptography-cipher-zipcrypto => ../../../goframework.io/cryptography-cipher-zipcrypto
	goframework.io/cryptography-hash-md4 => ../../../goframework.io/cryptography-hash-md4
	goframework.io/cryptography-kdf-7z => ../../../goframework.io/cryptography-kdf-7z
	goframework.io/cryptography-kdf-rar => ../../../goframework.io/cryptography-kdf-rar
	goframework.io/cryptography-kdf-sp800-108 => ../../../goframework.io/cryptography-kdf-sp800-108
	goframework.io/cryptography-mac-aes-cmac => ../../../goframework.io/cryptography-mac-aes-cmac
	goframework.io/desktop-ui => ../../../goframework.io/desktop-ui
	goframework.io/desktop-ui-a11y => ../../../goframework.io/desktop-ui-a11y
	goframework.io/desktop-ui-dock => ../../../goframework.io/desktop-ui-dock
	goframework.io/desktop-ui-icons => ../../../goframework.io/desktop-ui-icons
	goframework.io/desktop-ui-platform-all => ../../../goframework.io/desktop-ui-platform-all
	goframework.io/desktop-ui-platform-appkit => ../../../goframework.io/desktop-ui-platform-appkit
	goframework.io/desktop-ui-platform-freedesktop => ../../../goframework.io/desktop-ui-platform-freedesktop
	goframework.io/desktop-ui-platform-wayland => ../../../goframework.io/desktop-ui-platform-wayland
	goframework.io/desktop-ui-platform-win32 => ../../../goframework.io/desktop-ui-platform-win32
	goframework.io/desktop-ui-platform-x11 => ../../../goframework.io/desktop-ui-platform-x11
	goframework.io/desktop-ui-rack => ../../../goframework.io/desktop-ui-rack
	goframework.io/desktop-ui-richtext => ../../../goframework.io/desktop-ui-richtext
	goframework.io/desktop-ui-samples => ../../../goframework.io/desktop-ui-samples
	goframework.io/desktop-ui-skingen => ../../../goframework.io/desktop-ui-skingen
	goframework.io/desktop-ui-skins => ../../../goframework.io/desktop-ui-skins
	goframework.io/desktop-ui-text-complex => ../../../goframework.io/desktop-ui-text-complex
	goframework.io/desktop-ui-themes-adwaita => ../../../goframework.io/desktop-ui-themes-adwaita
	goframework.io/desktop-ui-themes-aero => ../../../goframework.io/desktop-ui-themes-aero
	goframework.io/desktop-ui-themes-all => ../../../goframework.io/desktop-ui-themes-all
	goframework.io/desktop-ui-themes-amiga => ../../../goframework.io/desktop-ui-themes-amiga
	goframework.io/desktop-ui-themes-aqua => ../../../goframework.io/desktop-ui-themes-aqua
	goframework.io/desktop-ui-themes-beos => ../../../goframework.io/desktop-ui-themes-beos
	goframework.io/desktop-ui-themes-bluecurve => ../../../goframework.io/desktop-ui-themes-bluecurve
	goframework.io/desktop-ui-themes-breeze => ../../../goframework.io/desktop-ui-themes-breeze
	goframework.io/desktop-ui-themes-clearlooks => ../../../goframework.io/desktop-ui-themes-clearlooks
	goframework.io/desktop-ui-themes-flatlaf => ../../../goframework.io/desktop-ui-themes-flatlaf
	goframework.io/desktop-ui-themes-fluent => ../../../goframework.io/desktop-ui-themes-fluent
	goframework.io/desktop-ui-themes-fusion => ../../../goframework.io/desktop-ui-themes-fusion
	goframework.io/desktop-ui-themes-hangar => ../../../goframework.io/desktop-ui-themes-hangar
	goframework.io/desktop-ui-themes-kde1 => ../../../goframework.io/desktop-ui-themes-kde1
	goframework.io/desktop-ui-themes-kde2 => ../../../goframework.io/desktop-ui-themes-kde2
	goframework.io/desktop-ui-themes-keramik => ../../../goframework.io/desktop-ui-themes-keramik
	goframework.io/desktop-ui-themes-luna => ../../../goframework.io/desktop-ui-themes-luna
	goframework.io/desktop-ui-themes-macos => ../../../goframework.io/desktop-ui-themes-macos
	goframework.io/desktop-ui-themes-material => ../../../goframework.io/desktop-ui-themes-material
	goframework.io/desktop-ui-themes-metal => ../../../goframework.io/desktop-ui-themes-metal
	goframework.io/desktop-ui-themes-metro => ../../../goframework.io/desktop-ui-themes-metro
	goframework.io/desktop-ui-themes-motif => ../../../goframework.io/desktop-ui-themes-motif
	goframework.io/desktop-ui-themes-next => ../../../goframework.io/desktop-ui-themes-next
	goframework.io/desktop-ui-themes-nimbus => ../../../goframework.io/desktop-ui-themes-nimbus
	goframework.io/desktop-ui-themes-openlook => ../../../goframework.io/desktop-ui-themes-openlook
	goframework.io/desktop-ui-themes-os2 => ../../../goframework.io/desktop-ui-themes-os2
	goframework.io/desktop-ui-themes-oxygen => ../../../goframework.io/desktop-ui-themes-oxygen
	goframework.io/desktop-ui-themes-plastik => ../../../goframework.io/desktop-ui-themes-plastik
	goframework.io/desktop-ui-themes-platinum => ../../../goframework.io/desktop-ui-themes-platinum
	goframework.io/desktop-ui-themes-system7 => ../../../goframework.io/desktop-ui-themes-system7
	goframework.io/desktop-ui-themes-web => ../../../goframework.io/desktop-ui-themes-web
	goframework.io/desktop-ui-themes-win31 => ../../../goframework.io/desktop-ui-themes-win31
	goframework.io/desktop-ui-themes-win95 => ../../../goframework.io/desktop-ui-themes-win95
	goframework.io/desktop-ui-tools => ../../../goframework.io/desktop-ui-tools
	goframework.io/desktop-ui-widgets => ../../../goframework.io/desktop-ui-widgets
	goframework.io/dsa-algorithms-diff => ../../../goframework.io/dsa-algorithms-diff
	goframework.io/dsa-collections-map-concurrent => ../../../goframework.io/dsa-collections-map-concurrent
	goframework.io/dsa-collections-map-sorted => ../../../goframework.io/dsa-collections-map-sorted
	goframework.io/dsa-collections-queue-concurrent => ../../../goframework.io/dsa-collections-queue-concurrent
	goframework.io/dsa-collections-queue-priority => ../../../goframework.io/dsa-collections-queue-priority
	goframework.io/dsa-collections-queue-ring => ../../../goframework.io/dsa-collections-queue-ring
	goframework.io/dsa-collections-set => ../../../goframework.io/dsa-collections-set
	goframework.io/events => ../../../goframework.io/events
	goframework.io/flow => ../../../goframework.io/flow
	goframework.io/graphics-paint2d => ../../../goframework.io/graphics-paint2d
	goframework.io/graphics-paint2d-gpu-gles => ../../../goframework.io/graphics-paint2d-gpu-gles
	goframework.io/logging-adapters-slog => ../../../goframework.io/logging-adapters-slog
	goframework.io/logging-advanced => ../../../goframework.io/logging-advanced
	goframework.io/logging-config => ../../../goframework.io/logging-config
	goframework.io/logging-config-network => ../../../goframework.io/logging-config-network
	goframework.io/logging-dispatchers-async => ../../../goframework.io/logging-dispatchers-async
	goframework.io/logging-dispatchers-buffered => ../../../goframework.io/logging-dispatchers-buffered
	goframework.io/logging-dispatchers-groupsort => ../../../goframework.io/logging-dispatchers-groupsort
	goframework.io/logging-dispatchers-multi => ../../../goframework.io/logging-dispatchers-multi
	goframework.io/logging-encoders-json => ../../../goframework.io/logging-encoders-json
	goframework.io/logging-encoders-text => ../../../goframework.io/logging-encoders-text
	goframework.io/logging-filters-onlylevel => ../../../goframework.io/logging-filters-onlylevel
	goframework.io/logging-filters-redact => ../../../goframework.io/logging-filters-redact
	goframework.io/logging-filters-sampling => ../../../goframework.io/logging-filters-sampling
	goframework.io/logging-filters-tolevel => ../../../goframework.io/logging-filters-tolevel
	goframework.io/logging-persisters-console => ../../../goframework.io/logging-persisters-console
	goframework.io/logging-persisters-consolewcolors => ../../../goframework.io/logging-persisters-consolewcolors
	goframework.io/logging-persisters-file => ../../../goframework.io/logging-persisters-file
	goframework.io/logging-persisters-gelf => ../../../goframework.io/logging-persisters-gelf
	goframework.io/logging-persisters-journald => ../../../goframework.io/logging-persisters-journald
	goframework.io/logging-persisters-network => ../../../goframework.io/logging-persisters-network
	goframework.io/logging-persisters-otlp => ../../../goframework.io/logging-persisters-otlp
	goframework.io/logging-persisters-rollingfile => ../../../goframework.io/logging-persisters-rollingfile
	goframework.io/logging-persisters-syslog => ../../../goframework.io/logging-persisters-syslog
	goframework.io/logging-persisters-websocket => ../../../goframework.io/logging-persisters-websocket
	goframework.io/logging-persisters-wineventlog => ../../../goframework.io/logging-persisters-wineventlog
	goframework.io/logging-persisters-writer => ../../../goframework.io/logging-persisters-writer
	goframework.io/logging-samples => ../../../goframework.io/logging-samples
	goframework.io/logging-spec => ../../../goframework.io/logging-spec
	goframework.io/logging-styles-classic => ../../../goframework.io/logging-styles-classic
	goframework.io/logging-styles-context => ../../../goframework.io/logging-styles-context
	goframework.io/logging-styles-withfields => ../../../goframework.io/logging-styles-withfields
	goframework.io/metrics-adapters-logging => ../../../goframework.io/metrics-adapters-logging
	goframework.io/metrics-collectors-runtime => ../../../goframework.io/metrics-collectors-runtime
	goframework.io/metrics-exporters-console => ../../../goframework.io/metrics-exporters-console
	goframework.io/metrics-exporters-memory => ../../../goframework.io/metrics-exporters-memory
	goframework.io/metrics-exporters-otlp => ../../../goframework.io/metrics-exporters-otlp
	goframework.io/metrics-exporters-prometheus => ../../../goframework.io/metrics-exporters-prometheus
	goframework.io/metrics-exporters-statsd => ../../../goframework.io/metrics-exporters-statsd
	goframework.io/metrics-spec => ../../../goframework.io/metrics-spec
	goframework.io/metrics-tracing => ../../../goframework.io/metrics-tracing
	goframework.io/metrics-tracing-http => ../../../goframework.io/metrics-tracing-http
	goframework.io/networking-connections => ../../../goframework.io/networking-connections
	goframework.io/networking-dns => ../../../goframework.io/networking-dns
	goframework.io/networking-http-client => ../../../goframework.io/networking-http-client
	goframework.io/networking-http-server => ../../../goframework.io/networking-http-server
	goframework.io/networking-http-signatures => ../../../goframework.io/networking-http-signatures
	goframework.io/networking-ssh => ../../../goframework.io/networking-ssh
	goframework.io/networking-ssh-server => ../../../goframework.io/networking-ssh-server
	goframework.io/networking-websocket => ../../../goframework.io/networking-websocket
	goframework.io/resilience => ../../../goframework.io/resilience
	goframework.io/system-dbus => ../../../goframework.io/system-dbus
	goframework.io/system-info-all => ../../../goframework.io/system-info-all
	goframework.io/system-info-cli => ../../../goframework.io/system-info-cli
	goframework.io/system-info-source-freebsd => ../../../goframework.io/system-info-source-freebsd
	goframework.io/system-info-source-linux => ../../../goframework.io/system-info-source-linux
	goframework.io/system-info-source-macos => ../../../goframework.io/system-info-source-macos
	goframework.io/system-info-source-netbsd => ../../../goframework.io/system-info-source-netbsd
	goframework.io/system-info-source-openbsd => ../../../goframework.io/system-info-source-openbsd
	goframework.io/system-info-source-shared-bsd => ../../../goframework.io/system-info-source-shared-bsd
	goframework.io/system-info-source-shared-cpuid => ../../../goframework.io/system-info-source-shared-cpuid
	goframework.io/system-info-source-shared-devicetree => ../../../goframework.io/system-info-source-shared-devicetree
	goframework.io/system-info-source-shared-edid => ../../../goframework.io/system-info-source-shared-edid
	goframework.io/system-info-source-shared-ids => ../../../goframework.io/system-info-source-shared-ids
	goframework.io/system-info-source-shared-input => ../../../goframework.io/system-info-source-shared-input
	goframework.io/system-info-source-shared-smbios => ../../../goframework.io/system-info-source-shared-smbios
	goframework.io/system-info-source-windows => ../../../goframework.io/system-info-source-windows
	goframework.io/system-info-spec => ../../../goframework.io/system-info-spec
	goframework.io/system-native-bsd => ../../../goframework.io/system-native-bsd
	goframework.io/system-native-macos => ../../../goframework.io/system-native-macos
	goframework.io/system-native-windows => ../../../goframework.io/system-native-windows
	goframework.io/system-process-all => ../../../goframework.io/system-process-all
	goframework.io/system-process-freebsd => ../../../goframework.io/system-process-freebsd
	goframework.io/system-process-linux => ../../../goframework.io/system-process-linux
	goframework.io/system-process-macos => ../../../goframework.io/system-process-macos
	goframework.io/system-process-monitor => ../../../goframework.io/system-process-monitor
	goframework.io/system-process-netbsd => ../../../goframework.io/system-process-netbsd
	goframework.io/system-process-openbsd => ../../../goframework.io/system-process-openbsd
	goframework.io/system-process-samples => ../../../goframework.io/system-process-samples
	goframework.io/system-process-spec => ../../../goframework.io/system-process-spec
	goframework.io/system-process-windows => ../../../goframework.io/system-process-windows
	goframework.io/system-service-all => ../../../goframework.io/system-service-all
	goframework.io/system-service-cli => ../../../goframework.io/system-service-cli
	goframework.io/system-service-freebsd => ../../../goframework.io/system-service-freebsd
	goframework.io/system-service-launchd => ../../../goframework.io/system-service-launchd
	goframework.io/system-service-netbsd => ../../../goframework.io/system-service-netbsd
	goframework.io/system-service-openbsd => ../../../goframework.io/system-service-openbsd
	goframework.io/system-service-openrc => ../../../goframework.io/system-service-openrc
	goframework.io/system-service-spec => ../../../goframework.io/system-service-spec
	goframework.io/system-service-systemd => ../../../goframework.io/system-service-systemd
	goframework.io/system-service-sysv => ../../../goframework.io/system-service-sysv
	goframework.io/system-service-windows => ../../../goframework.io/system-service-windows
	goframework.io/terminal => ../../../goframework.io/terminal
	goframework.io/terminal-colors => ../../../goframework.io/terminal-colors
	goframework.io/terminal-progress-line => ../../../goframework.io/terminal-progress-line
	goframework.io/terminal-progress-samples => ../../../goframework.io/terminal-progress-samples
	goframework.io/terminal-progress-spec => ../../../goframework.io/terminal-progress-spec
	goframework.io/terminal-progress-tree => ../../../goframework.io/terminal-progress-tree
	goframework.io/terminal-pty => ../../../goframework.io/terminal-pty
	goframework.io/terminal-ui => ../../../goframework.io/terminal-ui
	goframework.io/terminal-ui-samples => ../../../goframework.io/terminal-ui-samples
	goframework.io/terminal-ui-serve-ssh => ../../../goframework.io/terminal-ui-serve-ssh
	goframework.io/terminal-ui-serve-web => ../../../goframework.io/terminal-ui-serve-web
	goframework.io/terminal-ui-text => ../../../goframework.io/terminal-ui-text
	goframework.io/terminal-ui-widgets => ../../../goframework.io/terminal-ui-widgets
	goframework.io/trycatch => ../../../goframework.io/trycatch
	goframework.io/vfs => ../../../goframework.io/vfs
	goframework.io/vfs-device-partitions => ../../../goframework.io/vfs-device-partitions
	goframework.io/vfs-device-qcow2 => ../../../goframework.io/vfs-device-qcow2
	goframework.io/vfs-device-vhd => ../../../goframework.io/vfs-device-vhd
	goframework.io/vfs-device-vmdk => ../../../goframework.io/vfs-device-vmdk
	goframework.io/vfs-operations => ../../../goframework.io/vfs-operations
	goframework.io/vfs-source-archive-7z => ../../../goframework.io/vfs-source-archive-7z
	goframework.io/vfs-source-archive-rar => ../../../goframework.io/vfs-source-archive-rar
	goframework.io/vfs-source-archive-tar => ../../../goframework.io/vfs-source-archive-tar
	goframework.io/vfs-source-archive-zip => ../../../goframework.io/vfs-source-archive-zip
	goframework.io/vfs-source-disk => ../../../goframework.io/vfs-source-disk
	goframework.io/vfs-source-filesystem-exfat => ../../../goframework.io/vfs-source-filesystem-exfat
	goframework.io/vfs-source-filesystem-ext4 => ../../../goframework.io/vfs-source-filesystem-ext4
	goframework.io/vfs-source-filesystem-fat => ../../../goframework.io/vfs-source-filesystem-fat
	goframework.io/vfs-source-filesystem-iso9660 => ../../../goframework.io/vfs-source-filesystem-iso9660
	goframework.io/vfs-source-filesystem-ntfs => ../../../goframework.io/vfs-source-filesystem-ntfs
	goframework.io/vfs-source-filesystem-udf => ../../../goframework.io/vfs-source-filesystem-udf
	goframework.io/vfs-source-memory => ../../../goframework.io/vfs-source-memory
	goframework.io/vfs-source-remote-ftp => ../../../goframework.io/vfs-source-remote-ftp
	goframework.io/vfs-source-remote-gdrive => ../../../goframework.io/vfs-source-remote-gdrive
	goframework.io/vfs-source-remote-http => ../../../goframework.io/vfs-source-remote-http
	goframework.io/vfs-source-remote-nfs => ../../../goframework.io/vfs-source-remote-nfs
	goframework.io/vfs-source-remote-s3 => ../../../goframework.io/vfs-source-remote-s3
	goframework.io/vfs-source-remote-sftp => ../../../goframework.io/vfs-source-remote-sftp
	goframework.io/vfs-source-remote-smb => ../../../goframework.io/vfs-source-remote-smb
	goframework.io/vfs-source-remote-webdav => ../../../goframework.io/vfs-source-remote-webdav
	goframework.io/vfs-watch => ../../../goframework.io/vfs-watch
	goframework.io/xtend-channel => ../../../goframework.io/xtend-channel
	goframework.io/xtend-errors => ../../../goframework.io/xtend-errors
	goframework.io/xtend-io => ../../../goframework.io/xtend-io
	goframework.io/xtend-json => ../../../goframework.io/xtend-json
	goframework.io/xtend-math => ../../../goframework.io/xtend-math
	goframework.io/xtend-random => ../../../goframework.io/xtend-random
	goframework.io/xtend-strings => ../../../goframework.io/xtend-strings
	goframework.io/xtend-sync => ../../../goframework.io/xtend-sync
	goframework.io/xtend-time => ../../../goframework.io/xtend-time
	goframework.io/xtend-user => ../../../goframework.io/xtend-user
)
