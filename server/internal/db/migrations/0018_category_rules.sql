-- 0018_category_rules.sql: a builtin flag on classification rules, three new
-- categories and a much wider seed (docs/DATABASE.md, "Program-type classification").
--
-- Never clobbers an admin: new rows are only INSERTed where (match_type, match_value)
-- is free, and the seed corrections below only touch rows that still carry
-- builtin = true, i.e. are exactly as a seed migration wrote them. Every name here is
-- a generic, publicly documented executable base name (lower case, no ".exe").
--
-- New categories: remote (remote access and support), security (AV/EDR, VPN clients,
-- password managers) and utility (drivers, OEM services, updaters, sync and backup
-- agents). system, security and utility are background overhead: the usage
-- breakdowns leave them out.

-- builtin: shipped by a seed migration and not edited since. The API clears it on any
-- admin edit; later seed migrations may change only builtin rows.
ALTER TABLE process_category ADD COLUMN IF NOT EXISTS builtin boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN process_category.builtin IS
  'true = shipped by a seed migration and unchanged since; cleared by any admin edit';

-- Mark the 0004/0014 seed rows that are still exactly at their seed value.
UPDATE process_category pc SET builtin = true
FROM (VALUES
  ('browser','exact','chrome',10), ('browser','exact','msedge',10), ('browser','exact','firefox',10), ('browser','exact','brave',10),
  ('browser','exact','opera',10), ('browser','exact','vivaldi',10), ('browser','exact','iexplore',10), ('browser','exact','chrome_proxy',10),
  ('office','exact','winword',10), ('office','exact','excel',10), ('office','exact','powerpnt',10), ('office','exact','outlook',10),
  ('office','exact','onenote',10), ('office','exact','msaccess',10), ('office','exact','mspub',10), ('office','exact','visio',10),
  ('office','exact','winproj',10), ('office','exact','soffice',10), ('office','exact','soffice.bin',10), ('office','exact','wps',10),
  ('office','exact','et',10), ('office','exact','wpp',10), ('office','exact','acrord32',10), ('office','exact','acrobat',10),
  ('dev','exact','code',10), ('dev','exact','devenv',10), ('dev','exact','sublime_text',10), ('dev','exact','windowsterminal',10),
  ('dev','exact','wt',10), ('dev','exact','powershell',10), ('dev','exact','pwsh',10), ('dev','exact','cmd',10),
  ('dev','exact','conemu',10), ('dev','exact','git',10), ('dev','exact','git-bash',10), ('dev','exact','docker',10),
  ('dev','exact','postman',10), ('dev','exact','dbeaver',10), ('dev','exact','ssms',10), ('dev','prefix','idea',50),
  ('dev','prefix','pycharm',50), ('dev','prefix','goland',50), ('dev','prefix','webstorm',50), ('dev','prefix','clion',50),
  ('dev','prefix','rider',50), ('dev','prefix','python',50), ('dev','prefix','java',50), ('dev','prefix','node',50),
  ('media','exact','vlc',10), ('media','exact','spotify',10), ('media','exact','wmplayer',10), ('media','exact','groove',10),
  ('media','exact','potplayer',10), ('media','exact','potplayermini64',10), ('media','exact','foobar2000',10), ('media','exact','itunes',10),
  ('media','prefix','mpc-hc',50), ('comms','exact','teams',10), ('comms','exact','ms-teams',10), ('comms','exact','slack',10),
  ('comms','exact','zoom',10), ('comms','exact','discord',10), ('comms','exact','skype',10), ('comms','exact','whatsapp',10),
  ('comms','exact','telegram',10), ('comms','exact','thunderbird',10), ('system','exact','explorer',10), ('system','exact','dwm',10),
  ('system','exact','svchost',10), ('system','exact','services',10), ('system','exact','lsass',10), ('system','exact','csrss',10),
  ('system','exact','wininit',10), ('system','exact','winlogon',10), ('system','exact','smss',10), ('system','exact','system',10),
  ('system','exact','taskhostw',10), ('system','exact','spoolsv',10), ('system','exact','searchindexer',10), ('system','exact','sihost',10),
  ('system','exact','runtimebroker',10), ('system','exact','fontdrvhost',10), ('system','exact','ctfmon',10), ('system','exact','conhost',10),
  ('system','exact','dllhost',10), ('system','exact','msmpeng',10), ('system','exact','audiodg',10), ('system','exact','securityhealthservice',10),
  ('system','exact','wudfhost',10), ('system','exact','tolikopa-agent',10), ('system','exact','systemd',10), ('system','exact','kthreadd',10)
) AS s(category, match_type, match_value, priority)
WHERE pc.match_type = s.match_type AND pc.match_value = s.match_value
  AND pc.category = s.category AND pc.priority = s.priority AND pc.enabled AND NOT pc.builtin;

-- Seed corrections (builtin rows only).
-- Defender's engine and health service are security software, not the OS.
UPDATE process_category SET category = 'security', updated_at = now()
WHERE builtin AND category = 'system' AND match_type = 'exact'
  AND match_value IN ('msmpeng', 'securityhealthservice');

-- cmd / powershell on a managed fleet are overwhelmingly script hosts (logon scripts,
-- GPO, management agents), not development work. pwsh and the terminals stay in dev.
UPDATE process_category SET category = 'system', updated_at = now()
WHERE builtin AND category = 'dev' AND match_type = 'exact'
  AND match_value IN ('cmd', 'powershell');

-- New rules (572). exact = 10, prefix = 50 (on a tie the longer prefix wins), glob = 90.
INSERT INTO process_category (category, match_type, match_value, priority, builtin) VALUES
  -- browser
  --   browsers
  ('browser','exact','chromium',10,true), ('browser','exact','chromium-browser',10,true),
  ('browser','exact','msedge_proxy',10,true), ('browser','exact','firefox-esr',10,true),
  ('browser','exact','firefox-bin',10,true), ('browser','exact','waterfox',10,true),
  ('browser','exact','librewolf',10,true), ('browser','exact','floorp',10,true),
  ('browser','exact','arc',10,true),
  -- office
  --   Microsoft 365
  ('office','exact','olk',10,true), ('office','exact','onenoteim',10,true),
  ('office','exact','pbidesktop',10,true), ('office','exact','todo',10,true),
  ('office','exact','microsoft.notes',10,true), ('office','exact','m365copilot',10,true),
  ('office','exact','copilot',10,true),
  --   PDF
  ('office','exact','acrocef',10,true), ('office','exact','rdrcef',10,true),
  ('office','exact','acrord64',10,true), ('office','exact','sumatrapdf',10,true),
  ('office','exact','pdfxedit',10,true), ('office','exact','evince',10,true),
  ('office','exact','okular',10,true),
  --   PDF suites
  ('office','prefix','foxit',50,true), ('office','prefix','pdf24',50,true),
  --   notes, documents, AI assistants, CAD and other work apps
  ('office','exact','notepad',10,true), ('office','exact','wordpad',10,true),
  ('office','exact','calculatorapp',10,true), ('office','exact','calc',10,true),
  ('office','exact','snippingtool',10,true), ('office','exact','notion',10,true),
  ('office','exact','obsidian',10,true), ('office','exact','evernote',10,true),
  ('office','exact','xmind',10,true), ('office','exact','draw.io',10,true),
  ('office','exact','desktopeditors',10,true), ('office','exact','deepl',10,true),
  ('office','exact','chatgpt',10,true), ('office','exact','claude',10,true),
  ('office','exact','tableau',10,true), ('office','exact','saplogon',10,true),
  ('office','exact','acad',10,true), ('office','exact','revit',10,true),
  ('office','exact','sketchup',10,true),
  --   Power Query (Excel / Power BI)
  ('office','glob','microsoft.mashup.*',90,true),
  --   Power Automate Desktop
  ('office','prefix','pad.',50,true),
  -- dev
  --   editors, terminals, SCM, transfer and database tools
  ('dev','exact','notepad++',10,true), ('dev','exact','cursor',10,true),
  ('dev','exact','windsurf',10,true), ('dev','exact','zed',10,true), ('dev','exact','vim',10,true),
  ('dev','exact','nvim',10,true), ('dev','exact','gvim',10,true), ('dev','exact','emacs',10,true),
  ('dev','exact','studio64',10,true), ('dev','exact','githubdesktop',10,true),
  ('dev','exact','sourcetree',10,true), ('dev','exact','gitkraken',10,true),
  ('dev','exact','putty',10,true), ('dev','exact','winscp',10,true), ('dev','exact','filezilla',10,true),
  ('dev','exact','mobaxterm',10,true), ('dev','exact','termius',10,true), ('dev','exact','wsl',10,true),
  ('dev','exact','wslhost',10,true), ('dev','exact','docker-desktop',10,true),
  ('dev','exact','docker desktop',10,true), ('dev','exact','iisexpress',10,true),
  ('dev','exact','com.docker.backend',10,true), ('dev','exact','com.docker.build',10,true),
  ('dev','exact','ollama',10,true), ('dev','exact','ollama app',10,true), ('dev','exact','codex',10,true),
  ('dev','exact','pgadmin4',10,true), ('dev','exact','heidisql',10,true),
  ('dev','exact','navicat',10,true), ('dev','exact','mysqlworkbench',10,true),
  ('dev','exact','azuredatastudio',10,true), ('dev','exact','rstudio',10,true),
  ('dev','exact','matlab',10,true), ('dev','exact','gnome-terminal-server',10,true),
  ('dev','exact','konsole',10,true), ('dev','exact','xterm',10,true), ('dev','exact','mintty',10,true),
  --   toolchains
  ('dev','exact','go',10,true), ('dev','exact','gopls',10,true), ('dev','exact','cargo',10,true),
  ('dev','exact','rustc',10,true), ('dev','exact','rust-analyzer',10,true),
  ('dev','exact','dotnet',10,true), ('dev','exact','msbuild',10,true),
  ('dev','exact','vbcscompiler',10,true), ('dev','exact','esbuild',10,true), ('dev','exact','php',10,true),
  ('dev','exact','ruby',10,true), ('dev','exact','gcc',10,true), ('dev','exact','clang',10,true),
  ('dev','exact','make',10,true), ('dev','exact','cmake',10,true), ('dev','exact','ninja',10,true),
  --   JetBrains IDEs and notebooks
  ('dev','prefix','datagrip',50,true), ('dev','prefix','phpstorm',50,true),
  ('dev','prefix','rubymine',50,true), ('dev','prefix','jupyter',50,true),
  --   Visual Studio helpers
  ('dev','glob','servicehub.*',90,true),
  -- media
  --   players, photos, creative tools and game launchers
  ('media','exact','photos',10,true), ('media','exact','microsoft.photos',10,true),
  ('media','exact','microsoft.media.player',10,true), ('media','exact','music.ui',10,true),
  ('media','exact','video.ui',10,true), ('media','exact','mspaint',10,true),
  ('media','exact','photoshop',10,true), ('media','exact','illustrator',10,true),
  ('media','exact','indesign',10,true), ('media','exact','afterfx',10,true),
  ('media','exact','adobe premiere pro',10,true), ('media','exact','lightroom',10,true),
  ('media','exact','capcut',10,true), ('media','exact','canva',10,true), ('media','exact','figma',10,true),
  ('media','exact','inkscape',10,true), ('media','exact','blender',10,true),
  ('media','exact','krita',10,true), ('media','exact','obs64',10,true), ('media','exact','obs',10,true),
  ('media','exact','audacity',10,true), ('media','exact','handbrake',10,true),
  ('media','exact','mpv',10,true), ('media','exact','aimp',10,true), ('media','exact','musicbee',10,true),
  ('media','exact','winamp',10,true), ('media','exact','i_view64',10,true),
  ('media','exact','xnview',10,true), ('media','exact','windowscamera',10,true),
  ('media','exact','steam',10,true), ('media','exact','steamwebhelper',10,true),
  ('media','exact','epicgameslauncher',10,true), ('media','exact','battle.net',10,true),
  ('media','prefix','gimp',50,true), ('media','prefix','kmplayer',50,true),
  ('media','prefix','mpc-be',50,true), ('media','prefix','spotify',50,true),
  -- comms
  --   chat and meetings
  ('comms','exact','msteams',10,true), ('comms','exact','lync',10,true), ('comms','exact','webex',10,true),
  ('comms','exact','ciscowebexstart',10,true), ('comms','exact','webexhost',10,true),
  ('comms','exact','signal',10,true), ('comms','exact','viber',10,true),
  ('comms','exact','rocket.chat',10,true), ('comms','exact','mattermost',10,true),
  ('comms','exact','phoneexperiencehost',10,true), ('comms','exact','yourphone',10,true),
  ('comms','exact','zoomwebviewhost',10,true),
  --   WhatsApp desktop (whatsapp.root, ...)
  ('comms','prefix','whatsapp',50,true),
  -- remote
  --   remote access and support tools
  ('remote','exact','anydesk',10,true), ('remote','exact','rustdesk',10,true),
  ('remote','exact','mstsc',10,true), ('remote','exact','msrdc',10,true),
  ('remote','exact','quickassist',10,true), ('remote','exact','vncviewer',10,true),
  ('remote','exact','vncserver',10,true), ('remote','exact','winvnc',10,true),
  ('remote','exact','tvnserver',10,true), ('remote','exact','tvnviewer',10,true),
  ('remote','exact','remoting_host',10,true), ('remote','exact','supremo',10,true),
  ('remote','exact','supremoservice',10,true), ('remote','exact','rserver3',10,true),
  ('remote','exact','radmin',10,true), ('remote','exact','parsec',10,true),
  ('remote','prefix','teamviewer',50,true), ('remote','prefix','screenconnect',50,true),
  -- security
  --   Microsoft Defender
  ('security','exact','nissrv',10,true), ('security','exact','mpdefendercoreservice',10,true),
  ('security','exact','securityhealthsystray',10,true), ('security','exact','securityhealthhost',10,true),
  ('security','exact','smartscreen',10,true), ('security','exact','mpcmdrun',10,true),
  ('security','exact','mpsigstub',10,true), ('security','exact','mrt',10,true),
  ('security','exact','mssense',10,true), ('security','exact','sensecncproxy',10,true),
  ('security','exact','sensendr',10,true), ('security','exact','senseir',10,true),
  --   McAfee / Trellix
  ('security','exact','mcshield',10,true), ('security','exact','masvc',10,true),
  ('security','exact','macmnsvc',10,true), ('security','exact','macompatsvc',10,true),
  ('security','exact','mctray',10,true), ('security','exact','mcscript_inuse',10,true),
  --   McAfee / Trellix
  ('security','prefix','mfe',50,true),
  --   other endpoint protection
  ('security','exact','ccsvchst',10,true), ('security','exact','avp',10,true),
  ('security','exact','avpui',10,true), ('security','exact','klnagent',10,true),
  ('security','exact','ekrn',10,true), ('security','exact','egui',10,true),
  ('security','exact','eguiproxy',10,true), ('security','exact','bdagent',10,true),
  ('security','exact','vsserv',10,true), ('security','exact','bdservicehost',10,true),
  ('security','exact','epsecurityservice',10,true), ('security','exact','epprotectedservice',10,true),
  ('security','exact','epintegrationservice',10,true), ('security','exact','epupdateservice',10,true),
  ('security','exact','sedservice',10,true), ('security','exact','sspservice',10,true),
  ('security','exact','mcsagent',10,true), ('security','exact','mcsclient',10,true),
  ('security','exact','ntrtscan',10,true), ('security','exact','tmlisten',10,true),
  ('security','exact','pccntmon',10,true), ('security','exact','tmbmsrv',10,true),
  ('security','exact','coreserviceshell',10,true), ('security','exact','avastsvc',10,true),
  ('security','exact','avastui',10,true), ('security','exact','avgsvc',10,true),
  ('security','exact','avgui',10,true), ('security','exact','afwserv',10,true),
  ('security','exact','wrsa',10,true), ('security','exact','cylancesvc',10,true),
  --   other endpoint protection
  ('security','prefix','sophos',50,true), ('security','prefix','csfalcon',50,true),
  ('security','prefix','sentinel',50,true), ('security','prefix','mbam',50,true),
  ('security','prefix','norton',50,true),
  --   VPN clients
  ('security','exact','fcdblog',10,true), ('security','exact','fcappdb',10,true),
  ('security','exact','tailscaled',10,true), ('security','exact','tailscale-ipn',10,true),
  ('security','exact','warp-svc',10,true), ('security','exact','openvpn',10,true),
  ('security','exact','openvpn-gui',10,true), ('security','exact','openvpnserv',10,true),
  ('security','exact','wireguard',10,true), ('security','exact','vpnui',10,true),
  ('security','exact','vpnagent',10,true), ('security','exact','pangpa',10,true),
  ('security','exact','pangps',10,true), ('security','exact','zstray',10,true),
  ('security','exact','zsatunnel',10,true), ('security','exact','zsaservice',10,true),
  --   FortiClient
  ('security','prefix','forti',50,true),
  --   password managers
  ('security','exact','keepass',10,true), ('security','exact','keepassxc',10,true),
  ('security','exact','1password',10,true), ('security','exact','bitwarden',10,true),
  -- utility
  --   updaters (googleupdate, microsoftedgeupdate, onedrivestandaloneupdater, ...)
  ('utility','glob','*updater',90,true), ('utility','glob','*update',90,true),
  --   updaters and installers
  ('utility','exact','updater',10,true), ('utility','exact','officeclicktorun',10,true),
  ('utility','exact','officec2rclient',10,true), ('utility','exact','onedrivesetup',10,true),
  ('utility','exact','adobearm',10,true), ('utility','exact','armsvc',10,true),
  ('utility','exact','jusched',10,true), ('utility','exact','jucheck',10,true),
  ('utility','exact','jaureg',10,true),
  --   Office background
  ('utility','exact','msoia',10,true), ('utility','exact','msosync',10,true),
  ('utility','exact','officebackgroundtaskhandler',10,true), ('utility','exact','sdxhelper',10,true),
  ('utility','exact','filecoauth',10,true),
  --   cloud sync and backup
  ('utility','exact','onedrive',10,true), ('utility','exact','onedrive.sync.service',10,true),
  ('utility','exact','googledrivefs',10,true), ('utility','exact','dropbox',10,true),
  ('utility','exact','nextcloud',10,true), ('utility','exact','owncloud',10,true),
  ('utility','exact','megasync',10,true), ('utility','exact','restic',10,true),
  ('utility','exact','cbvscservice11',10,true),
  --   backup
  ('utility','prefix','veeam',50,true),
  --   Adobe background
  ('utility','exact','creative cloud',10,true), ('utility','exact','creative cloud helper',10,true),
  ('utility','exact','creative cloud ui helper',10,true),
  ('utility','exact','adobe desktop service',10,true), ('utility','exact','coresync',10,true),
  ('utility','exact','ccxprocess',10,true), ('utility','exact','cclibrary',10,true),
  ('utility','exact','adobeipcbroker',10,true), ('utility','exact','adobegcclient',10,true),
  ('utility','exact','adobenotificationclient',10,true),
  ('utility','exact','adobenotificationhelper',10,true), ('utility','exact','adobecollabsync',10,true),
  ('utility','exact','adobe crash processor',10,true), ('utility','exact','cephtmlengine',10,true),
  ('utility','exact','cc_engine_x64',10,true), ('utility','exact','crashpad_handler',10,true),
  --   Intel
  ('utility','exact','esif_uf',10,true), ('utility','exact','ipf_uf',10,true),
  ('utility','exact','ipfsvc',10,true), ('utility','exact','ipf_helper',10,true),
  ('utility','exact','jhi_service',10,true), ('utility','exact','lms',10,true),
  ('utility','exact','xtuservice',10,true), ('utility','exact','igcc',10,true),
  ('utility','exact','oneapp.igcc.winservice',10,true), ('utility','exact','presentmonservice',10,true),
  ('utility','exact','ibtsiva',10,true), ('utility','exact','esrv',10,true),
  ('utility','exact','esrv_svc',10,true), ('utility','exact','dsaservice',10,true),
  ('utility','exact','sursvc',10,true), ('utility','exact','sysinfocap',10,true),
  ('utility','exact','diagscap',10,true), ('utility','exact','networkcap',10,true),
  ('utility','exact','apphelpercap',10,true), ('utility','exact','rstmwservice',10,true),
  ('utility','exact','iastordatamgrsvc',10,true),
  --   Intel
  ('utility','prefix','intel',50,true), ('utility','prefix','igfx',50,true),
  --   NVIDIA
  ('utility','exact','nvdisplay.container',10,true), ('utility','exact','nvcontainer',10,true),
  ('utility','exact','nvsphelper64',10,true), ('utility','exact','nvidia overlay',10,true),
  ('utility','exact','nvidia share',10,true), ('utility','exact','nvidia web helper',10,true),
  ('utility','exact','nvtelemetrycontainer',10,true), ('utility','exact','nvwmi64',10,true),
  ('utility','exact','nvcplui',10,true), ('utility','exact','nvidia app',10,true),
  --   AMD
  ('utility','exact','radeonsoftware',10,true), ('utility','exact','amdrsserv',10,true),
  ('utility','exact','atieclxx',10,true), ('utility','exact','atiesrxx',10,true),
  ('utility','exact','amdfendrsr',10,true), ('utility','exact','amdrssrcext',10,true),
  ('utility','exact','amdow',10,true),
  --   audio, input and other drivers
  ('utility','exact','ravbg64',10,true), ('utility','exact','rtkngui64',10,true),
  ('utility','exact','dax3api',10,true), ('utility','exact','fmservice64',10,true),
  ('utility','exact','fmaudiomonitor',10,true), ('utility','exact','fmtoastnotification',10,true),
  ('utility','exact','fmapp',10,true), ('utility','exact','cxaudiosvc',10,true),
  ('utility','exact','cxutilsvc',10,true), ('utility','exact','syntpenh',10,true),
  ('utility','exact','syntphelper',10,true), ('utility','exact','etdctrl',10,true),
  ('utility','exact','mtkbtsvc',10,true),
  --   audio, input and other drivers
  ('utility','prefix','rtk',50,true), ('utility','prefix','elevoc',50,true),
  ('utility','prefix','waves',50,true), ('utility','prefix','nahimic',50,true),
  ('utility','prefix','elan',50,true), ('utility','prefix','logioptions',50,true),
  ('utility','prefix','logiplugin',50,true), ('utility','prefix','logitech',50,true),
  ('utility','prefix','lghub',50,true), ('utility','prefix','logibolt',50,true),
  --   OEM services
  ('utility','exact','touchpointanalyticsclientservice',10,true),
  ('utility','exact','supportassistagent',10,true),
  --   OEM services (HP, Dell, Lenovo, ASUS)
  ('utility','prefix','hp',50,true), ('utility','prefix','hotkeyservice',50,true),
  ('utility','prefix','lanwlanwwanswitchingservice',50,true), ('utility','prefix','dell',50,true),
  ('utility','prefix','lenovo',50,true), ('utility','prefix','lnv',50,true),
  ('utility','prefix','asus',50,true), ('utility','prefix','armourycrate',50,true),
  --   virtual machine guest tools
  ('utility','exact','vmtoolsd',10,true), ('utility','exact','vgauthservice',10,true),
  ('utility','exact','vm3dservice',10,true), ('utility','exact','vmwaretray',10,true),
  ('utility','exact','vboxservice',10,true), ('utility','exact','vboxtray',10,true),
  ('utility','exact','qemu-ga',10,true), ('utility','exact','spice-vdagent',10,true),
  --   other vendor helpers
  ('utility','exact','wpscloudsvr',10,true), ('utility','exact','updaterui',10,true),
  ('utility','exact','adobeupdateservice',10,true), ('utility','exact','microsoft.cmdpal.ui',10,true),
  ('utility','exact','bingwallpaper',10,true), ('utility','exact','winrar',10,true),
  ('utility','exact','7zfm',10,true), ('utility','exact','7zg',10,true),
  --   Autodesk background services and PowerToys
  ('utility','prefix','adsk',50,true), ('utility','prefix','powertoys',50,true),
  -- system
  --   Windows shell and session
  ('system','exact','registry',10,true), ('system','exact','memory compression',10,true),
  ('system','exact','system idle process',10,true), ('system','exact','secure system',10,true),
  ('system','exact','lsaiso',10,true), ('system','exact','ngciso',10,true),
  ('system','exact','bioiso',10,true), ('system','exact','logonui',10,true),
  ('system','exact','lockapp',10,true), ('system','exact','lockapphost',10,true),
  ('system','exact','applicationframehost',10,true), ('system','exact','systemsettings',10,true),
  ('system','exact','systemsettingsbroker',10,true), ('system','exact','shellhost',10,true),
  ('system','exact','textinputhost',10,true), ('system','exact','startmenuexperiencehost',10,true),
  ('system','exact','shellexperiencehost',10,true), ('system','exact','searchhost',10,true),
  ('system','exact','searchapp',10,true), ('system','exact','searchui',10,true),
  ('system','exact','searchprotocolhost',10,true), ('system','exact','searchfilterhost',10,true),
  ('system','exact','screenclippinghost',10,true), ('system','exact','useroobebroker',10,true),
  ('system','exact','accountscontrolhost',10,true),
  ('system','exact','microsoft.aad.brokerplugin',10,true), ('system','exact','widgets',10,true),
  ('system','exact','widgetservice',10,true), ('system','exact','crossdeviceservice',10,true),
  ('system','exact','crossdeviceresume',10,true), ('system','exact','dashost',10,true),
  ('system','exact','dataexchangehost',10,true), ('system','exact','aggregatorhost',10,true),
  ('system','exact','backgroundtaskhost',10,true), ('system','exact','wudfcompanionhost',10,true),
  ('system','exact','unsecapp',10,true), ('system','exact','wmiprvse',10,true),
  ('system','exact','wmiadap',10,true), ('system','exact','wmiapsrv',10,true),
  ('system','exact','wmiregistrationservice',10,true), ('system','exact','vds',10,true),
  ('system','exact','wlanext',10,true), ('system','exact','rdpclip',10,true),
  ('system','exact','gamebar',10,true), ('system','exact','gamebarftserver',10,true),
  ('system','exact','gamebarpresencewriter',10,true), ('system','exact','storedesktopextension',10,true),
  ('system','exact','winstore.app',10,true), ('system','exact','windowspackagemanagerserver',10,true),
  ('system','exact','appvshnotify',10,true), ('system','exact','comppkgsrv',10,true),
  ('system','exact','taskmgr',10,true), ('system','exact','perfmon',10,true),
  ('system','exact','mmc',10,true), ('system','exact','inetmgr',10,true),
  --   WebView2: a shared runtime inside widgets, search and Office, and filing it under browser overstated web use
  ('system','exact','msedgewebview2',10,true),
  --   Windows servicing and maintenance
  ('system','exact','tiworker',10,true), ('system','exact','trustedinstaller',10,true),
  ('system','exact','mousocoreworker',10,true), ('system','exact','monotificationux',10,true),
  ('system','exact','musnotification',10,true), ('system','exact','musnotifyicon',10,true),
  ('system','exact','wuauclt',10,true), ('system','exact','usoclient',10,true),
  ('system','exact','sihclient',10,true), ('system','exact','compattelrunner',10,true),
  ('system','exact','devicecensus',10,true), ('system','exact','wimserv',10,true),
  ('system','exact','dismhost',10,true), ('system','exact','sdbinst',10,true),
  ('system','exact','msiexec',10,true), ('system','exact','rundll32',10,true),
  ('system','exact','mshta',10,true), ('system','exact','mscorsvw',10,true),
  ('system','exact','ngen',10,true), ('system','exact','ngentask',10,true),
  ('system','exact','sppsvc',10,true), ('system','exact','sppextcomobj',10,true),
  ('system','exact','sgrmbroker',10,true), ('system','exact','werfault',10,true),
  ('system','exact','wermgr',10,true), ('system','exact','cleanmgr',10,true),
  ('system','exact','defrag',10,true), ('system','exact','srtasks',10,true),
  ('system','exact','vssvc',10,true), ('system','exact','backgrounddownload',10,true),
  ('system','exact','wuaucltcore',10,true), ('system','exact','musnotificationux',10,true),
  ('system','exact','uhssvc',10,true), ('system','exact','softlandingtask',10,true),
  ('system','exact','managementagenthost',10,true),
  --   Windows update packages (windows-kb890830-x64-...)
  ('system','glob','windows-kb*',90,true),
  --   Windows Server roles and server software
  ('system','exact','dns',10,true), ('system','exact','dfsrs',10,true),
  ('system','exact','dfssvc',10,true), ('system','exact','ismserv',10,true),
  ('system','exact','ntfrs',10,true), ('system','exact','msdtc',10,true),
  ('system','exact','w3wp',10,true), ('system','exact','inetinfo',10,true),
  ('system','exact','sqlwriter',10,true), ('system','exact','sqlbrowser',10,true),
  ('system','exact','sqlagent',10,true), ('system','exact','sqlservr',10,true),
  ('system','exact','sqlceip',10,true), ('system','exact','fdlauncher',10,true),
  ('system','exact','fdhost',10,true), ('system','exact','msmdsrv',10,true),
  ('system','exact','msdtssrvr',10,true), ('system','exact','smsvchost',10,true),
  ('system','exact','servermanager',10,true), ('system','exact','postgres',10,true),
  ('system','exact','mysqld',10,true), ('system','exact','mongod',10,true),
  ('system','exact','redis-server',10,true), ('system','exact','dockerd',10,true),
  ('system','exact','containerd',10,true), ('system','exact','containerd-shim-runc-v2',10,true),
  ('system','exact','docker-proxy',10,true), ('system','exact','caddy',10,true),
  ('system','exact','httpd',10,true), ('system','exact','nginx',10,true),
  ('system','exact','vmmem',10,true), ('system','exact','vmmemwsl',10,true),
  ('system','exact','vmcompute',10,true), ('system','exact','vmwp',10,true),
  ('system','exact','wslservice',10,true),
  --   Linux daemons and desktop session
  ('system','exact','dbus-daemon',10,true), ('system','exact','dbus-broker',10,true),
  ('system','exact','polkitd',10,true), ('system','exact','cron',10,true),
  ('system','exact','crond',10,true), ('system','exact','atd',10,true), ('system','exact','sshd',10,true),
  ('system','exact','rsyslogd',10,true), ('system','exact','snapd',10,true),
  ('system','exact','packagekitd',10,true), ('system','exact','networkmanager',10,true),
  ('system','exact','wpa_supplicant',10,true), ('system','exact','dhclient',10,true),
  ('system','exact','avahi-daemon',10,true), ('system','exact','cupsd',10,true),
  ('system','exact','udisksd',10,true), ('system','exact','upowerd',10,true),
  ('system','exact','accounts-daemon',10,true), ('system','exact','thermald',10,true),
  ('system','exact','irqbalance',10,true), ('system','exact','chronyd',10,true),
  ('system','exact','ntpd',10,true), ('system','exact','agetty',10,true),
  ('system','exact','login',10,true), ('system','exact','multipathd',10,true),
  ('system','exact','fwupd',10,true), ('system','exact','unattended-upgr',10,true),
  ('system','exact','gnome-shell',10,true), ('system','exact','xorg',10,true),
  ('system','exact','xwayland',10,true), ('system','exact','pipewire',10,true),
  ('system','exact','pipewire-pulse',10,true), ('system','exact','wireplumber',10,true),
  ('system','exact','pulseaudio',10,true), ('system','exact','gdm',10,true),
  ('system','exact','lightdm',10,true), ('system','exact','sddm',10,true),
  ('system','exact','plasmashell',10,true), ('system','exact','kwin_x11',10,true),
  ('system','exact','kwin_wayland',10,true), ('system','exact','nautilus',10,true),
  ('system','exact','dolphin',10,true),
  --   Linux kernel threads (the server stores kworker/0:1 as kworker)
  ('system','exact','kworker',10,true), ('system','exact','ksoftirqd',10,true),
  ('system','exact','migration',10,true), ('system','exact','cpuhp',10,true),
  ('system','exact','idle_inject',10,true), ('system','exact','irq',10,true),
  ('system','exact','jbd2',10,true), ('system','exact','watchdog',10,true),
  ('system','exact','rcu_sched',10,true), ('system','exact','rcu_preempt',10,true),
  ('system','exact','khungtaskd',10,true), ('system','exact','oom_reaper',10,true),
  ('system','exact','kauditd',10,true), ('system','exact','kdevtmpfs',10,true),
  ('system','exact','khugepaged',10,true), ('system','exact','ksmd',10,true),
  ('system','exact','kblockd',10,true), ('system','exact','writeback',10,true),
  --   Linux
  ('system','prefix','systemd-',50,true), ('system','prefix','kswapd',50,true),
  ('system','prefix','kcompactd',50,true),
  --   Linux kernel RCU threads
  ('system','glob','rcu_*',90,true)
ON CONFLICT (match_type, match_value) DO NOTHING;

-- Further generic names seen unclassified on a production fleet (installers, Windows
-- shell/print helpers, Microsoft server roles, Linux CLI tools). Same rules as above:
-- exact = 10, prefix = 50, glob = 90; free (match_type, match_value) slots only.
INSERT INTO process_category (category, match_type, match_value, priority, builtin) VALUES
  -- office
  ('office','exact','chatgpt classic',10,true), ('office','exact','mscopilot',10,true),
  ('office','exact','onenotem',10,true), ('office','exact','trello',10,true),
  ('office','exact','texstudio',10,true), ('office','exact','pdflatex',10,true),
  -- dev
  ('dev','exact','docker-buildx',10,true), ('dev','exact','docker-compose',10,true),
  ('dev','exact','wslrelay',10,true), ('dev','exact','openconsole',10,true),
  ('dev','exact','iisexpresstray',10,true), ('dev','exact','fzsftp',10,true),
  ('dev','exact','jp2launcher',10,true), ('dev','exact','ssh',10,true),
  -- media
  ('media','exact','applemusic',10,true), ('media','exact','amplibraryagent',10,true),
  ('media','exact','clipchamp',10,true), ('media','exact','ffmpeg',10,true),
  ('media','exact','smartpsslite',10,true),
  --   Hikvision iVMS-4200 CCTV client (ivms-4200.video.c, ...)
  ('media','prefix','ivms-4200',50,true),
  -- comms
  ('comms','exact','cpthost',10,true), ('comms','exact','yourphoneappproxyhost',10,true),
  -- security
  ('security','exact','cloudflare warp',10,true), ('security','exact','pangphip',10,true),
  ('security','exact','wa_3rd_party_host_32',10,true), ('security','exact','wa_3rd_party_host_64',10,true),
  ('security','exact','mcchsvc',10,true), ('security','exact','mcupdatermodule',10,true),
  ('security','exact','mc-webview-cnt',10,true), ('security','exact','sacsrv',10,true),
  ('security','exact','sacmonitor',10,true),
  --   Microsoft Defender helpers (defendersessionhelper, defenderaiplatformhost, ...)
  ('security','prefix','defender',50,true),
  -- utility
  --   installers and their temp copies
  ('utility','exact','setup',10,true), ('utility','exact','installer',10,true),
  ('utility','exact','update_task',10,true), ('utility','exact','vc_redist.x64',10,true),
  ('utility','exact','vc_redist.x86',10,true), ('utility','exact','courgette64',10,true),
  ('utility','exact','elevation_service',10,true), ('utility','exact','rdrservicesupdater2_x64',10,true),
  ('utility','exact','creativecloudset-up',10,true), ('utility','exact','genuineservice',10,true),
  ('utility','prefix','adobe_licensing',50,true), ('utility','prefix','acrordrdc',50,true),
  ('utility','prefix','microsoftedge_x64_',50,true), ('utility','prefix','microsoftedgeupdate',50,true),
  ('utility','prefix','codesetup-',50,true),
  ('utility','glob','*_chrome_installer*',90,true), ('utility','glob','*.tmp',90,true),
  --   drivers, vendor helpers and small tools
  ('utility','exact','syntpenhservice',10,true), ('utility','exact','secomn64',10,true),
  ('utility','exact','secocl64',10,true), ('utility','exact','smartaudio3',10,true),
  ('utility','exact','diracaudsrv',10,true), ('utility','exact','igcctray',10,true),
  ('utility','exact','logi_crashpad_handler',10,true), ('utility','exact','fnplicensingservice64',10,true),
  ('utility','exact','browser_assistant',10,true), ('utility','exact','cefsharp.browsersubprocess',10,true),
  ('utility','exact','figma_agent',10,true), ('utility','exact','speedtest',10,true),
  ('utility','exact','lightshot',10,true),
  ('utility','prefix','vmware',50,true),
  -- system
  --   Windows shell, servicing and print helpers
  ('system','exact','upfc',10,true), ('system','exact','appactions',10,true),
  ('system','exact','disksnapshot',10,true), ('system','exact','waasmedicagent',10,true),
  ('system','exact','werfaultsecure',10,true), ('system','exact','pickerhost',10,true),
  ('system','exact','credentialuibroker',10,true), ('system','exact','tabtip',10,true),
  ('system','exact','consent',10,true), ('system','exact','drvinst',10,true),
  ('system','exact','systemsettingsadminflows',10,true), ('system','exact','wsmprovhost',10,true),
  ('system','exact','backgroundtransferhost',10,true), ('system','exact','mobsync',10,true),
  ('system','exact','prevhost',10,true), ('system','exact','firstlogonanim',10,true),
  ('system','exact','winsat',10,true), ('system','exact','wmic',10,true),
  ('system','exact','tasklist',10,true), ('system','exact','msfeedssync',10,true),
  ('system','exact','ielowutil',10,true), ('system','exact','wwahost',10,true),
  ('system','exact','identity_helper',10,true), ('system','exact','webexperiencehostapp',10,true),
  ('system','exact','windowsinternal.composableshell.experiences.textinput.inputapp',10,true),
  ('system','exact','printdialog',10,true), ('system','exact','printisolationhost',10,true),
  ('system','exact','splwow64',10,true), ('system','exact','printfilterpipelinesvc',10,true),
  ('system','exact','printqueueactioncenter',10,true),
  --   Windows Server roles and Microsoft server products (Hyper-V, AD web services,
  --   FSRM, Dynamics CRM, SQL Server Reporting Services)
  ('system','exact','vmms',10,true), ('system','exact','microsoft.activedirectory.webservices',10,true),
  ('system','exact','srmhost',10,true), ('system','exact','crmasyncservice',10,true),
  ('system','exact','crmunzipservice',10,true), ('system','exact','mscrmmonitoringservice',10,true),
  ('system','exact','reportingservicesservice',10,true), ('system','exact','rshostingservice',10,true),
  ('system','exact','rsmanagement',10,true), ('system','exact','rsportal',10,true),
  ('system','prefix','microsoft.crm.',50,true), ('system','prefix','microsoft.reportingservices',50,true),
  --   other server software
  ('system','exact','apache',10,true), ('system','exact','rotatelogs',10,true),
  ('system','exact','mongo',10,true), ('system','exact','redisservice',10,true),
  ('system','exact','prtg probe',10,true), ('system','exact','prtg server',10,true),
  ('system','prefix','tomcat',50,true), ('system','prefix','spiceworks',50,true),
  --   Linux daemons, CLI tools and kernel threads
  ('system','exact','(udev-worker)',10,true), ('system','exact','watchdogd',10,true),
  ('system','exact','pool_workqueue_release',10,true), ('system','exact','modemmanager',10,true),
  ('system','exact','dpkg',10,true), ('system','exact','appstreamcli',10,true),
  ('system','exact','bash',10,true), ('system','exact','sh',10,true),
  ('system','exact','grep',10,true), ('system','exact','awk',10,true),
  ('system','exact','sed',10,true), ('system','exact','tail',10,true),
  ('system','exact','tar',10,true), ('system','exact','htop',10,true),
  ('system','glob','scsi_*',90,true)
ON CONFLICT (match_type, match_value) DO NOTHING;
