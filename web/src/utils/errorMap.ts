/**
 * 错误码映射表
 * 将后端返回的错误码映射为友好的中文提示
 */

export interface ErrorMap {
  [key: number | string]: string
}

// 通用错误码
export const COMMON_ERRORS: ErrorMap = {
  // HTTP 状态码
  400: '请求参数错误',
  401: '未授权，请先登录',
  403: '禁止访问',
  404: '请求的资源不存在',
  405: '请求方法不被允许',
  408: '请求超时',
  409: '资源冲突',
  413: '请求数据过大',
  415: '不支持的媒体类型',
  422: '请求参数验证失败',
  429: '请求过于频繁，请稍后再试',
  500: '服务器内部错误',
  502: '网关错误',
  503: '服务暂时不可用',
  504: '网关超时',

  // 业务错误码 (1000-1999: 认证相关)
  1001: '用户名或密码错误',
  1002: '用户名已存在',
  1003: '密码强度不足',
  1004: '验证码错误',
  1005: '验证码已过期',
  1006: '账号已被锁定',
  1007: '账号已过期',
  1008: '登录失败次数过多，请稍后再试',
  1009: 'Token 已过期',
  1010: 'Token 无效',
  1011: '刷新 Token 已过期',
  1012: 'MFA 验证失败',
  1013: 'MFA 未启用',

  // 业务错误码 (2000-2999: 任务相关)
  2001: '任务不存在',
  2002: '任务状态不允许此操作',
  2003: '任务创建失败',
  2004: '任务取消失败',
  2005: '任务下载失败',
  2006: '批量任务创建失败',
  2007: '批量任务不存在',
  2008: '任务队列已满',
  2009: '任务优先级无效',
  2010: '无效的 URL 格式',

  // 业务错误码 (3000-3999: 下载相关)
  3001: '下载引擎不可用',
  3002: '下载失败',
  3003: '文件不存在',
  3004: '文件下载失败',
  3005: '磁盘空间不足',
  3006: '下载超时',
  3007: '不支持的 URL 格式',
  3008: '视频格式不支持',
  3009: '视频清晰度不可用',

  // 业务错误码 (4000-4999: 系统相关)
  4001: '数据库错误',
  4002: '配置文件错误',
  4003: '系统资源不足',
  4004: '服务依赖不可用',
  4005: '日志记录失败',
}

// 错误消息映射（根据错误消息关键字）
export const ERROR_MESSAGES: ErrorMap = {
  'Invalid request body': '请求数据格式错误',
  'Invalid username or password': '用户名或密码错误',
  'Username already exists': '用户名已存在',
  'Password must be at least 8 characters': '密码长度至少为 8 位',
  'Failed to generate token': '生成令牌失败',
  'Invalid or expired token': '令牌无效或已过期',
  'Invalid or expired refresh token': '刷新令牌无效或已过期',
  'Missing authorization token': '缺少授权令牌',
  'Invalid authorization header format': '授权头格式错误',
  'Database error': '数据库错误',
  'Failed to create user': '创建用户失败',
  'LDAP authentication failed': 'LDAP 认证失败',
  'MFA already enabled': 'MFA 已启用',
  'MFA is required to view audit logs': '查看审计日志需要启用 MFA',
  'Verification code required': '需要验证码',
  'Invalid verification code': '验证码错误',
  'MFA enabled successfully': 'MFA 启用成功',
  'MFA disabled successfully': 'MFA 禁用成功',
  'Failed to enable MFA': '启用 MFA 失败',
  'Failed to disable MFA': '禁用 MFA 失败',
  'Failed to get user info': '获取用户信息失败',
  'Failed to generate MFA secret': '生成 MFA 密钥失败',
  'Failed to query audit logs': '查询审计日志失败',
}

/**
 * 获取友好的错误消息
 * @param errorCode 错误码或错误消息
 * @param fallback 默认消息
 * @returns 友好的错误消息
 */
export function getErrorMessage(errorCode: number | string, fallback = '操作失败'): string {
  // 首先尝试精确匹配错误码
  if (COMMON_ERRORS[errorCode]) {
    return COMMON_ERRORS[errorCode]
  }

  // 尝试匹配错误消息
  const errorCodeStr = String(errorCode)
  for (const [key, value] of Object.entries(ERROR_MESSAGES)) {
    if (errorCodeStr.includes(key)) {
      return value
    }
  }

  // 返回默认消息
  return fallback
}

/**
 * 根据 HTTP 状态码获取错误消息
 * @param status HTTP 状态码
 * @returns 友好的错误消息
 */
export function getHttpErrorStatus(status: number): string {
  return COMMON_ERRORS[status] || `未知错误 (${status})`
}

/**
 * 网络错误消息
 */
export const NETWORK_ERRORS: ErrorMap = {
  ECONNREFUSED: '无法连接到服务器，请检查网络或联系管理员',
  ETIMEDOUT: '连接超时，请检查网络',
  EHOSTUNREACH: '主机不可达',
  ENOTFOUND: '无法找到服务器',
  ERR_NETWORK: '网络错误',
  ECONNRESET: '连接被重置',
  EPIPE: '连接已关闭',
}

/**
 * 获取网络错误的友好消息
 * @param errorCode 网络错误代码
 * @returns 友好的错误消息
 */
export function getNetworkErrorMessage(errorCode: string): string {
  return NETWORK_ERRORS[errorCode] || '网络连接失败，请检查网络后重试'
}

export default {
  COMMON_ERRORS,
  ERROR_MESSAGES,
  NETWORK_ERRORS,
  getErrorMessage,
  getHttpErrorStatus,
  getNetworkErrorMessage,
}
