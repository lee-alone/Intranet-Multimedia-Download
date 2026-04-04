import { createRouter, createWebHistory, type RouteMeta } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import type { Component } from 'vue'

// 扩展路由元信息类型
interface CustomRouteMeta extends RouteMeta {
  requiresAuth?: boolean
  requiresGuest?: boolean
  title?: string
}

// 组件懒加载工厂函数（带类型）
const viewComponents: Record<string, () => Promise<Component>> = {
  Login: () => import('@/views/Login.vue'),
  Register: () => import('@/views/Register.vue'),
  Dashboard: () => import('@/views/Dashboard.vue'),
  Tasks: () => import('@/views/Tasks.vue'),
  NewTask: () => import('@/views/NewTask.vue'),
  Audit: () => import('@/views/Audit.vue'),
  Users: () => import('@/views/Users.vue'),
  Profile: () => import('@/views/Profile.vue'),
  Cookies: () => import('@/views/Cookies.vue'),
  NotFound: () => import('@/views/NotFound.vue'),
}

function lazyLoad(viewName: string): () => Promise<Component> {
  return viewComponents[viewName] || viewComponents.NotFound
}

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: lazyLoad('Login'),
    meta: {
      requiresGuest: true,
      title: '登录',
    } as CustomRouteMeta,
  },
  {
    path: '/register',
    name: 'Register',
    component: lazyLoad('Register'),
    meta: {
      requiresGuest: true,
      title: '注册',
    } as CustomRouteMeta,
  },
  {
    path: '/',
    name: 'Layout',
    component: () => import('@/layouts/MainLayout.vue'),
    meta: {
      requiresAuth: true,
      title: '首页',
    } as CustomRouteMeta,
    children: [
      {
        path: '',
        name: 'Dashboard',
        component: lazyLoad('Dashboard'),
        meta: {
          title: '仪表盘',
        } as CustomRouteMeta,
      },
      {
        path: 'tasks',
        name: 'Tasks',
        component: lazyLoad('Tasks'),
        meta: {
          title: '任务列表',
        } as CustomRouteMeta,
      },
      {
        path: 'tasks/new',
        name: 'NewTask',
        component: lazyLoad('NewTask'),
        meta: {
          title: '新建任务',
        } as CustomRouteMeta,
      },
      {
        path: 'audit',
        name: 'Audit',
        component: lazyLoad('Audit'),
        meta: {
          title: '审计日志',
        } as CustomRouteMeta,
      },
      {
        path: 'users',
        name: 'Users',
        component: lazyLoad('Users'),
        meta: {
          title: '用户管理',
        } as CustomRouteMeta,
      },
      {
        path: 'profile',
        name: 'Profile',
        component: lazyLoad('Profile'),
        meta: {
          title: '个人中心',
        } as CustomRouteMeta,
      },
      {
        path: 'cookies',
        name: 'Cookies',
        component: lazyLoad('Cookies'),
        meta: {
          title: 'Cookie 管理',
        } as CustomRouteMeta,
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: lazyLoad('NotFound'),
    meta: {
      title: '页面未找到',
    } as CustomRouteMeta,
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫 - 权限控制
router.beforeEach((to, _from, next) => {
  const token = localStorage.getItem('token')
  const isAuthenticated = !!token
  const meta = to.meta as CustomRouteMeta

  // 设置页面标题
  if (meta.title) {
    document.title = `${meta.title} - 校园资源采集系统`
  }

  // 需要认证但未登录 → 跳转登录页
  if (meta.requiresAuth && !isAuthenticated) {
    next({
      name: 'Login',
      query: { redirect: to.fullPath },
      replace: true,
    })
    return
  }

  // 已登录访问登录/注册页 → 跳转首页
  if ((to.name === 'Login' || to.name === 'Register') && isAuthenticated) {
    next({ name: 'Dashboard', replace: true })
    return
  }

  next()
})

// 路由后置钩子 - 页面滚动到顶部
router.afterEach(() => {
  window.scrollTo({ top: 0, behavior: 'smooth' })
})

export default router
