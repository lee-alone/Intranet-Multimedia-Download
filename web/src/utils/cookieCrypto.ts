/**
 * Cookie 加密工具类
 * 
 * 使用信封加密方案：
 * - RSA-OAEP (SHA-256) 加密 AES-256 Key（Web Crypto API 原生支持）
 * - AES-GCM 加密 Cookie 内容
 * 
 * 数据格式：[2字节 AES Key 长度] + [RSA-OAEP 加密的 AES Key 原始字节] + [12字节 IV] + [AES-GCM 密文 + Auth Tag]
 * 
 * ⚠️ 安全注意：
 * - RSA-OAEP 使用 SHA-256 哈希，与后端 rsa.DecryptOAEP(sha256.New(), ...) 完全匹配
 * - AES Key 直接传输原始 32 字节二进制数据，不做 Base64 转换
 */

/**
 * 将 Uint8Array 稳健地转换为 Base64 字符串
 * 
 * ⚠️ 使用循环拼接而非展开运算符（...envelope），避免大数据量下 Stack Overflow
 * 
 * @param bytes Uint8Array 二进制数据
 * @returns Base64 编码的字符串
 */
function uint8ArrayToBase64(bytes: Uint8Array): string {
  let binary = ''
  const len = bytes.byteLength
  for (let i = 0; i < len; i++) {
    binary += String.fromCharCode(bytes[i])
  }
  return window.btoa(binary)
}

/**
 * 将 PEM 公钥转换为 ArrayBuffer
 * 
 * @param pem PEM 格式的 RSA 公钥
 * @returns ArrayBuffer 格式的二进制数据
 */
function pemToArrayBuffer(pem: string): ArrayBuffer {
  // 去除 PEM 头和尾
  const base64 = pem
    .replace('-----BEGIN PUBLIC KEY-----', '')
    .replace('-----END PUBLIC KEY-----', '')
    .replace(/\s/g, '')
  
  // Base64 解码
  const binaryString = atob(base64)
  const bytes = new Uint8Array(binaryString.length)
  for (let i = 0; i < binaryString.length; i++) {
    bytes[i] = binaryString.charCodeAt(i)
  }
  
  return bytes.buffer
}

/**
 * 生成随机 AES-256 Key (32 字节)
 */
function generateAESKey(): Uint8Array {
  return crypto.getRandomValues(new Uint8Array(32))
}

/**
 * 使用 RSA-OAEP (SHA-256) 加密 AES Key
 * 
 * ⚠️ 使用 Web Crypto API 原生 RSA-OAEP，与后端 Go rsa.DecryptOAEP(sha256.New(), ...) 完全匹配
 * 
 * @param aesKey AES Key 原始字节 (Uint8Array)
 * @param publicKeyPEM PEM 格式的 RSA 公钥
 * @returns RSA-OAEP 加密后的密文 (Uint8Array)
 */
async function encryptAESKeyWithRSA(
  aesKey: Uint8Array,
  publicKeyPEM: string
): Promise<Uint8Array> {
  // 1. 将 PEM 公钥转换为 ArrayBuffer
  const keyBuffer = pemToArrayBuffer(publicKeyPEM)
  
  // 2. 导入公钥（SPKI 格式，RSA-OAEP with SHA-256）
  const rsaKey = await crypto.subtle.importKey(
    'spki',
    keyBuffer,
    {
      name: 'RSA-OAEP',
      hash: { name: 'SHA-256' }, // ⚠️ 必须与后端 sha256.New() 匹配
    },
    false, // 不可导出
    ['encrypt']
  )
  
  // 3. 使用 RSA-OAEP 直接加密 AES Key 的原始字节（32 字节）
  const encryptedBuffer = await crypto.subtle.encrypt(
    { name: 'RSA-OAEP' },
    rsaKey,
    aesKey // ⚠️ 直接传入原始 32 字节，不做 Base64 转换
  )
  
  return new Uint8Array(encryptedBuffer)
}

/**
 * 使用 AES-GCM 加密 Cookie 内容
 * 
 * @param content 原始 Cookie 文本
 * @param aesKey AES Key (Uint8Array)
 * @returns { iv: Uint8Array, ciphertext: Uint8Array }
 */
async function encryptWithAESGCM(
  content: string,
  aesKey: Uint8Array
): Promise<{ iv: Uint8Array; ciphertext: Uint8Array }> {
  // 生成 12 字节 IV (96 位，GCM 推荐长度)
  const iv = crypto.getRandomValues(new Uint8Array(12))
  
  // 导入 AES Key
  const cryptoKey = await crypto.subtle.importKey(
    'raw',
    aesKey,
    { name: 'AES-GCM' },
    false,
    ['encrypt']
  )
  
  // 加密
  const encoder = new TextEncoder()
  const encryptedBuffer = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv },
    cryptoKey,
    encoder.encode(content)
  )
  
  return {
    iv: iv,
    ciphertext: new Uint8Array(encryptedBuffer), // 包含 Auth Tag (16 字节)
  }
}

/**
 * 信封加密：组合 RSA-OAEP + AES-GCM
 * 
 * @param content 原始 Cookie 文本
 * @param publicKeyPEM PEM 格式的 RSA 公钥
 * @returns Base64 编码的完整密文
 */
export async function encryptCookie(
  content: string,
  publicKeyPEM: string
): Promise<string> {
  try {
    // 1. 生成随机 AES-256 Key (32 字节)
    const aesKey = generateAESKey()
    
    // 2. 使用 RSA-OAEP (SHA-256) 加密 AES Key（原始字节，不做 Base64）
    const encryptedAESKey = await encryptAESKeyWithRSA(aesKey, publicKeyPEM)
    
    // 3. 使用 AES-GCM 加密 Cookie 内容
    const { iv, ciphertext } = await encryptWithAESGCM(content, aesKey)
    
    // 4. 组装信封数据
    // 格式：[2字节 AES Key 长度] + [RSA-OAEP 加密的 AES Key] + [12字节 IV] + [AES-GCM 密文]
    const aesKeyLen = encryptedAESKey.length
    
    // AES Key 长度 (2 字节，大端序)
    const lengthBytes = new Uint8Array([
      (aesKeyLen >> 8) & 0xFF,
      aesKeyLen & 0xFF,
    ])
    
    // 将各部分拼接
    const envelope = new Uint8Array(
      lengthBytes.length + encryptedAESKey.length + iv.length + ciphertext.length
    )
    
    envelope.set(lengthBytes, 0)
    envelope.set(encryptedAESKey, lengthBytes.length)
    envelope.set(iv, lengthBytes.length + encryptedAESKey.length)
    envelope.set(ciphertext, lengthBytes.length + encryptedAESKey.length + iv.length)
    
    // 5. 转换为 Base64（改用稳健函数，避免大数据量下 Stack Overflow）
    return uint8ArrayToBase64(envelope)
  } catch (error) {
    console.error('Cookie 加密失败:', error)
    throw new Error('Cookie 加密失败，请检查内容是否过大')
  }
}

/**
 * 从 Cookie 文本中提取域名
 * 
 * Netscape Cookie 格式第一行是注释或域名
 * 例如：# Netscape HTTP Cookie File 或 .bilibili.com
 */
export function extractDomainFromCookie(content: string): string | null {
  const lines = content.split('\n')
  
  for (const line of lines) {
    const trimmed = line.trim()
    
    // 跳过空行和注释行
    if (!trimmed || trimmed.startsWith('#')) {
      continue
    }
    
    // Netscape 格式：domain\tflag\tpath\tsecure\texpiration\tname\tvalue
    const fields = trimmed.split('\t')
    if (fields.length >= 7) {
      let domain = fields[0].trim()
      
      // 去除前导点号
      if (domain.startsWith('.')) {
        domain = domain.substring(1)
      }
      
      return domain
    }
  }
  
  return null
}

/**
 * 验证 Cookie 格式是否符合 Netscape 规范
 */
export function validateCookieFormat(content: string): string | null {
  const lines = content.split('\n')
  let validLines = 0

  for (let i = 0; i < lines.length; i++) {
    const line = lines[i].trim()

    // 跳过空行和注释行
    if (!line || line.startsWith('#')) {
      continue
    }

    const fields = line.split('\t')
    if (fields.length !== 7) {
      return `第 ${i + 1} 行：应为 7 个制表符分隔的字段，实际 ${fields.length} 个`
    }

    if (!fields[0]) {
      return `第 ${i + 1} 行：域名为空`
    }

    // 验证过期时间是否为数字
    const expiration = fields[4].trim()
    if (isNaN(Number(expiration))) {
      return `第 ${i + 1} 行：无效的过期时间戳`
    }

    validLines++
  }

  if (validLines === 0) {
    return '未找到有效的 Cookie 条目'
  }

  return null // 验证通过
}

/**
 * 清理 Cookie 文本中的备注行（以 # 开头的注释行和空行）
 *
 * 某些浏览器插件导出的 Cookie 文件包含注释行，发送到后端可能导致 403 错误
 *
 * @param content 原始 Cookie 文本
 * @returns 清理后的 Cookie 文本（仅保留有效数据行）
 */
export function cleanCookieContent(content: string): string {
  const lines = content.split('\n')
  const cleanedLines = lines.filter(line => {
    const trimmed = line.trim()
    // 过滤掉空行和注释行
    return trimmed !== '' && !trimmed.startsWith('#')
  })
  return cleanedLines.join('\n')
}

/**
 * 智能过滤：根据目标域名自动筛选 Cookie 条目
 *
 * @param content 原始 Cookie 文本
 * @param targetDomain 目标域名（如 bilibili.com）
 * @returns { filteredContent: 过滤后的内容, totalCount: 总条数, filteredCount: 过滤后条数, domainStats: 域名统计 }
 */
export function filterCookieByDomain(
  content: string,
  targetDomain: string
): {
  filteredContent: string
  totalCount: number
  filteredCount: number
  domainStats: Record<string, number>
  expiresInfo: { earliest: number; latest: number } | null
} {
  const lines = content.split('\n')
  const targetClean = targetDomain.toLowerCase().replace(/^\./, '')
  const validLines: string[] = []
  const filteredLines: string[] = []
  const domainStats: Record<string, number> = {}
  let earliest = Infinity
  let latest = 0

  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('#')) continue

    const fields = trimmed.split('\t')
    if (fields.length !== 7) continue

    const domain = fields[0].trim().replace(/^\./, '')
    const expires = parseInt(fields[4].trim(), 10)

    // 统计域名分布
    domainStats[domain] = (domainStats[domain] || 0) + 1
    validLines.push(trimmed)

    // 更新过期时间范围
    if (!isNaN(expires)) {
      if (expires < earliest) earliest = expires
      if (expires > latest) latest = expires
    }

    // 域名匹配检查（支持双向匹配）
    if (isDomainRelated(domain, targetClean)) {
      filteredLines.push(trimmed)
    }
  }

  const expiresInfo =
    earliest !== Infinity && latest > 0
      ? { earliest, latest }
      : null

  return {
    filteredContent: filteredLines.join('\n'),
    totalCount: validLines.length,
    filteredCount: filteredLines.length,
    domainStats,
    expiresInfo,
  }
}

/**
 * 判断两个域名是否相关（支持双向匹配）
 *
 * @param domain Cookie 条目中的域名
 * @param expected 用户选择的目标域名
 * @returns 是否相关
 */
function isDomainRelated(domain: string, expected: string): boolean {
  // 1. 精确匹配
  if (domain === expected) return true
  // 2. 子域名 → 父域名（如 www.bilibili.com 匹配 bilibili.com）
  if (domain.endsWith('.' + expected)) return true
  // 3. 父域名 → 子域名（如 bilibili.com 匹配 www.bilibili.com）
  if (expected.endsWith('.' + domain)) return true
  return false
}
