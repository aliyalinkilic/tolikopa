// Turkish — shared components.

export const range = {
  '1h': { label: '1 saat', short: '1s' },
  '6h': { label: '6 saat', short: '6s' },
  '24h': { label: '24 saat', short: '24s' },
  '7d': { label: '7 gün', short: '7g' },
  '30d': { label: '30 gün', short: '30g' },
  custom: { label: 'Özel aralık', short: 'Özel' },
}

export const ui = {
  table: {
    empty: 'Kayıt bulunamadı',
    prev: 'Önceki',
    next: 'Sonraki',
    prevPage: 'Önceki sayfa',
    nextPage: 'Sonraki sayfa',
  },
  range: {
    group: 'Zaman aralığı',
    customDialog: 'Özel zaman aralığı',
    from: 'Başlangıç',
    to: 'Bitiş',
    invalid: 'Başlangıç, bitişten önce olmalı.',
  },
  heatmap: {
    caption: 'Haftanın günü ve saate göre aktif dakikalar ısı haritası',
    hour: 'Saat {hour}',
    none: 'Yok',
    low: 'Az',
    high: 'Çok',
  },
  timeline: {
    empty: 'Bu aralıkta oturum yok',
    sessions: 'Oturumlar',
    active: 'Aktif',
    loggedOn: 'Oturum Açık',
    ongoing: 'devam ediyor',
  },
  chart: {
    empty: 'Bu aralıkta veri yok',
    samples: '{count} örnek',
    noCategory: 'Kategori verisi yok',
  },
  confirm: {
    typeNumber: 'Onaylamak için aşağıdaki sayıyı yazın',
  },
  machineCard: {
    updates: '{count} güncelleme',
    reboot: 'Yeniden Başlatma',
    openDetail: '{host} makine detayını aç',
  },
  alertList: {
    none: 'Açık alarm yok',
    allGood: 'Her şey yolunda görünüyor.',
    ack: 'Onayla',
  },
  password: {
    ruleLength: 'En az {count} karakter',
    ruleMatch: 'İki parola aynı',
    ruleDifferent: 'Mevcut paroladan farklı',
  },
  copy: {
    failed: 'Kopyalanamadı — metni elle seçip kopyalayın.',
  },
}
