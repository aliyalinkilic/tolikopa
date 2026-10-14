// Turkish — shared vocabulary, formatting, shell, errors and wire-enum labels.
// Capitalisation: "Her Kelime Büyük" for nav, tabs, titles, buttons, table headers,
// field labels and badges; sentence case for descriptions, hints, toasts, empty
// states and aria-labels.

export const common = {
  appName: 'ToliKOPA',
  tagline: 'BT Filo İzleme',
  loading: 'Yükleniyor…',
  save: 'Kaydet',
  cancel: 'İptal',
  close: 'Kapat',
  confirm: 'Onayla',
  dismiss: 'Vazgeç',
  delete: 'Sil',
  edit: 'Düzenle',
  add: 'Ekle',
  create: 'Oluştur',
  apply: 'Uygula',
  retry: 'Yeniden Dene',
  copy: 'Kopyala',
  copied: 'Kopyalandı',
  download: 'İndir',
  undo: 'Geri Al',
  all: 'Tümü',
  none: 'Yok',
  unknown: 'Bilinmiyor',
  online: 'Çevrimiçi',
  offline: 'Çevrimdışı',
  live: 'Canlı',
  now: 'Şimdi',
  enabled: 'Etkin',
  disabled: 'Devre Dışı',
  noIssues: 'Sorun Yok',
  review: 'İnceleyin',
  machine: 'Makine',
  user: 'Kullanıcı',
  category: 'Kategori',
  status: 'Durum',
  lastSeen: 'Son Görülme',
  firstSeen: 'İlk Görülme',
  lastSeenAgo: 'Son görülme {when}',
  cpu: 'CPU',
  ram: 'RAM',
  disk: 'Disk',
  memory: 'Bellek',
  noSession: 'Oturum yok',
  role: {
    admin: 'Yönetici',
    viewer: 'Görüntüleyici',
  },
  theme: {
    label: 'Tema',
    dark: 'Koyu',
    light: 'Açık',
    system: 'Sistem',
    hint: '“Sistem”, işletim sisteminizin açık/koyu tercihini canlı olarak izler.',
  },
  language: {
    label: 'Dil',
    hint: 'Seçim hemen uygulanır ve hesabınıza kaydedilir.',
  },
  machines: '{count} makine',
  users: '{count} kullanıcı',
  alerts: '{count} alarm',
  criticalCount: '{count} kritik',
  offlineCount: '{count} çevrimdışı',
  recordCount: '{count} kayıt',
  days: '{count} gün',
  cores: '{count} çekirdek',
  devices: '{count} cihaz',
  untagged: 'Etiketsiz',
}

export const format = {
  soon: 'az sonra',
  justNow: 'az önce',
  minutesAgo: '{count} dk önce',
  hoursAgo: '{count} sa önce',
  daysAgo: '{count} gün önce',
  perSecond: '{value}/sn',
  percent: '%{value}',
  unit: {
    day: '{n} g',
    hour: '{n} sa',
    minute: '{n} dk',
    second: '{n} sn',
  },
}

export const nav = {
  overview: 'Panel',
  devices: 'Cihazlar',
  slowMachines: 'Yavaş Makineler',
  users: 'Kullanıcılar',
  departments: 'Departmanlar',
  alerts: 'Alarmlar',
  settings: 'Ayarlar',
  setup: 'Kurulum',
  profile: 'Profil',
  machineDetail: 'Makine Detayı',
  userDetail: 'Kullanıcı Detayı',
  login: 'Oturum Aç',
  notFound: 'Sayfa Bulunamadı',
}

export const shell = {
  closeMenu: 'Menüyü kapat',
  openMenu: 'Menüyü aç',
  expandSidebar: 'Kenar çubuğunu genişlet',
  collapseSidebar: 'Kenar çubuğunu daralt',
  collapse: 'Daralt',
  mainNav: 'Ana gezinme',
  searchPlaceholder: 'Cihaz veya kullanıcı ara…',
  search: 'Ara',
  liveOn: 'Canlı bağlantı etkin',
  liveOff: 'Bağlantı kuruluyor',
  connecting: 'Bağlanıyor…',
  userMenu: 'Kullanıcı menüsü',
  profile: 'Profilim',
  signOut: 'Çıkış Yap',
  version: 'Sürüm {version}',
  notFoundTitle: 'Sayfa bulunamadı',
  notFoundBody: 'Aradığınız sayfa taşınmış veya hiç var olmamış olabilir.',
  backHome: 'Panele Dön',
  dismissToast: 'Bildirimi kapat',
  adminRequiredTitle: 'Yönetici erişimi gerekli',
}

// Server error codes (`{code, message}`) → text. `message` is the fallback for a
// code this table does not know.
export const errors = {
  invalid_request: 'İstek geçersiz. Girdiğiniz bilgileri kontrol edin.',
  invalid_requestField: 'İstek geçersiz: “{field}” alanını kontrol edin.',
  invalid_credentials: 'Kullanıcı adı veya parola hatalı.',
  account_disabled: 'Bu hesap devre dışı bırakılmış. Bir yöneticiye başvurun.',
  session_required: 'Oturum gerekli. Lütfen yeniden giriş yapın.',
  session_invalid: 'Oturumunuzun süresi doldu. Lütfen yeniden giriş yapın.',
  csrf_failed: 'Güvenlik doğrulaması başarısız oldu. Sayfayı yenileyip yeniden deneyin.',
  admin_required: 'Bu işlem için yönetici yetkisi gerekir.',
  rate_limited: 'Çok fazla deneme yapıldı. Lütfen bir süre bekleyin.',
  rate_limitedWait: 'Çok fazla deneme yapıldı. {duration} sonra yeniden deneyin.',
  forbidden: 'Bu işlem için yetkiniz yok.',
  mfa_setup_required: 'Bekleyen bir kurulum yok. İki adımlı doğrulama kurulumunu baştan başlatın.',
  mfa_invalid: 'Doğrulama kodu hatalı veya süresi dolmuş.',
  mfa_token_invalid: 'Doğrulama adımının süresi doldu. Lütfen yeniden giriş yapın.',
  mfa_already_enabled: 'İki adımlı doğrulama zaten etkin.',
  mfa_not_enabled: 'İki adımlı doğrulama etkin değil.',
  password_too_weak: 'Parola çok kısa. En az {min} karakter kullanın.',
  password_same: 'Yeni parola mevcut paroladan farklı olmalı.',
  password_too_long: 'Parola çok uzun. En çok {max} bayt olabilir.',
  password_incorrect: 'Mevcut parola hatalı.',
  username_taken: 'Bu kullanıcı adı zaten kullanılıyor.',
  user_not_found: 'Kullanıcı bulunamadı.',
  last_admin: 'Son yönetici hesabı kaldırılamaz, devre dışı bırakılamaz veya yetkisi düşürülemez.',
  cannot_modify_self: 'Bu işlemi kendi hesabınız üzerinde yapamazsınız.',
  enable_own_2fa_first: 'Bu ayarı açmadan önce kendi hesabınızda iki adımlı doğrulamayı etkinleştirin.',
  mfa_enrollment_required: 'Kurumunuz iki adımlı doğrulamayı zorunlu kılıyor. Devam etmek için kurulumu tamamlayın.',
  mfa_required_by_policy: 'Kurumunuz iki adımlı doğrulamayı zorunlu kıldığı için kapatılamaz.',
  setup_already_done: 'Kurulum zaten yapılmış: bu sunucuda bir yönetici hesabı var.',
  setup_code_invalid: 'Kurulum kodu hatalı.',
  setup_incomplete: 'Kurulum bilgileri eksik.',
  setup_incompleteMissing: 'Kurulum bilgileri eksik: {missing}.',
  artifacts_unavailable: 'Ajan dosyaları bu sunucuda bulunmuyor: sunucu imajı ajan derlemesi olmadan oluşturulmuş.',
  not_found: 'Kayıt bulunamadı.',
  method_not_allowed: 'Bu işlem desteklenmiyor.',
  payload_too_large: 'İstek çok büyük.',
  unsupported_media_type: 'İstek biçimi desteklenmiyor.',
  category_rule_conflict: 'Aynı eşleşme türü ve değeriyle bir kural zaten var.',
  too_many_items: 'Tek istekte çok fazla öğe seçildi.',
  too_many_itemsMax: 'Tek istekte en çok {max} öğe işlenebilir.',
  dry_run_required: 'Önce deneme modunda çalıştırıp en az bir sonuç gözlemleyin.',
  unknown_catalog_item: 'Seçilen öğe katalogda yok. Sayfayı yenileyip yeniden deneyin.',
  server_busy: 'Sunucu şu anda meşgul. Lütfen biraz sonra yeniden deneyin.',
  server_error: 'Sunucu hatası. Lütfen daha sonra yeniden deneyin.',
  // Client-side fallbacks, keyed by what went wrong rather than by a server code.
  network: 'Sunucuya ulaşılamadı. Bağlantınızı kontrol edin.',
  http_400: 'Geçersiz istek.',
  http_403: 'Bu işlem için yetkiniz yok.',
  http_404: 'Kayıt bulunamadı.',
  http_409: 'Çakışma: kayıt bu arada değişmiş.',
  http_413: 'İstek çok büyük.',
  http_429: 'Çok fazla istek — lütfen bir süre bekleyin.',
  http_5xx: 'Sunucu hatası.',
  generic: 'İstek başarısız oldu.',
}

export const category = {
  browser: 'Tarayıcı',
  office: 'Ofis',
  dev: 'Geliştirme',
  media: 'Medya',
  comms: 'İletişim',
  system: 'Sistem',
  other: 'Diğer',
}

export const session = {
  active: 'Aktif',
  idle: 'Boşta',
  locked: 'Kilitli',
  disconnected: 'Bağlantı Kesildi',
  none: 'Oturum Yok',
}

export const severity = {
  crit: 'Kritik',
  warn: 'Uyarı',
  info: 'Bilgi',
}

export const alertStatus = {
  open: 'Açık',
  ack: 'Onaylandı',
  resolved: 'Çözüldü',
}

export const eventKind = {
  app_crash: 'Uygulama Çökmesi',
  app_hang: 'Uygulama Yanıt Vermiyor',
  hw_warning: 'Donanım Uyarısı',
  smart_fail: 'Disk SMART Hatası',
}

export const rule = {
  offline: 'Çevrimdışı',
  cpu_high: 'Yüksek CPU',
  mem_high: 'Yüksek Bellek',
  ram_high: 'Yüksek Bellek',
  disk_low: 'Düşük Disk Alanı',
  disk_full: 'Disk Doluyor',
  smart_fail: 'Disk Sağlığı (SMART)',
  crash_storm: 'Çökme Fırtınası',
  app_crash: 'Uygulama Çökmesi',
  net_high: 'Yüksek Ağ Trafiği',
}

// What each rule's thresholds measure — shown under the rule name in Settings.
export const ruleHelp = {
  offline: 'Makinenin görülmediği süre (saniye).',
  cpu_high: 'Süre boyunca ortalama CPU kullanımı (%).',
  mem_high: 'Süre boyunca ortalama bellek kullanımı (%).',
  ram_high: 'Süre boyunca ortalama bellek kullanımı (%).',
  disk_low: 'En dolu birimde kalan boş alan (%).',
  disk_full: 'En dolu birimin doluluk oranı (%).',
  smart_fail: 'Diskin SMART öz denetimi başarısız olduğunda tetiklenir; eşik kullanmaz.',
  crash_storm: 'Süre penceresindeki uygulama çökmesi sayısı.',
  app_crash: 'Süre penceresindeki uygulama çökmesi sayısı.',
  net_high: 'Süre boyunca ortalama ağ trafiği.',
}

// Alert sentences built from `rule` + `params`. Each rule has a full form and a
// bare form used when the params it needs are missing.
export const alertText = {
  offline: '{host} çevrimdışı ({duration} görülmedi)',
  offlineBare: '{host} çevrimdışı',
  cpu_high: '{host} üzerinde CPU kullanımı yüksek: {value}',
  cpu_highBare: '{host} üzerinde CPU kullanımı yüksek',
  mem_high: '{host} üzerinde bellek kullanımı yüksek: {value}',
  mem_highBare: '{host} üzerinde bellek kullanımı yüksek',
  disk_low: '{host} {mount} diskinde boş alan az ({value} boş)',
  disk_lowNoMount: '{host} üzerinde boş disk alanı az ({value} boş)',
  disk_lowBare: '{host} üzerinde boş disk alanı az',
  smart_fail: '{host} {disk} diski SMART hatası bildiriyor',
  smart_failBare: '{host} üzerinde bir disk SMART hatası bildiriyor',
  crash_storm: '{host} son {duration} içinde {count} kez çöktü',
  crash_stormNoWindow: '{host} üzerinde {count} uygulama çökmesi',
  crash_stormBare: '{host} üzerinde sık uygulama çökmesi',
  net_high: '{host} üzerinde ağ trafiği yüksek: {value}',
  net_highBare: '{host} üzerinde ağ trafiği yüksek',
  thisMachine: 'Bu makine',
}

export const slowness = {
  severity: {
    crit: 'Kritik',
    warn: 'Riskli',
    ok: 'İyi Durumda',
  },
  reason: {
    mem_pressure: { label: 'Sürekli Bellek Baskısı', describe: 'ortalama {value} bellek kullanımı' },
    low_ram: { label: 'Yetersiz RAM', describe: 'toplam {value} GB' },
    disk_full: { label: 'Disk Doluyor', describe: '{value} dolu' },
    weak_cpu: { label: 'Zayıf İşlemci', describe: '{value} çekirdek' },
    smart: { label: 'Disk Sağlığı Riskli', describe: 'SMART uyarısı veriyor' },
    crashes: { label: 'Sık Uygulama Çökmesi', describe: '{value} çökme/donma' },
    long_uptime: { label: 'Uzun Süredir Yeniden Başlatılmadı', describe: '{value} gündür açık' },
  },
}
