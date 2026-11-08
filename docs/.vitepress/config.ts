import { defineConfig } from 'vitepress'

export default defineConfig({
  title: "OpenBridge",
  description: "A universal bridge connecting AI agents with everything.",
  base: '/open-bridge/',
  sitemap: {
    hostname: 'https://nomagicln.github.io/open-bridge/'
  },

  head: [
    ['link', { rel: 'icon', href: '/open-bridge/logo.jpeg' }],
    ['meta', { name: 'algolia-site-verification', content: '3886DFB52573D01F' }]
  ],



  themeConfig: {
    logo: '/logo.jpeg',

    // Shared social links
    socialLinks: [
      { icon: 'github', link: 'https://github.com/nomagicln/open-bridge' }
    ],

    // Search is shared but can be localized inside
    search: {
      provider: 'algolia',
      options: {
        appId: process.env.ALGOLIA_APP_ID || '',
        apiKey: process.env.ALGOLIA_API_KEY || '', // Must be the Search-Only API Key
        indexName: 'open-bridge',
        locales: {
          zh: {
            placeholder: '搜索文档',
            translations: {
              button: {
                buttonText: '搜索文档',
                buttonAriaLabel: '搜索文档'
              },
              modal: {
                searchBox: {
                  resetButtonTitle: '清除查询条件',
                  resetButtonAriaLabel: '清除查询条件',
                  cancelButtonText: '取消',
                  cancelButtonAriaLabel: '取消'
                },
                startScreen: {
                  recentSearchesTitle: '搜索历史',
                  noRecentSearchesText: '没有搜索历史',
                  saveRecentSearchButtonTitle: '保存至搜索历史',
                  removeRecentSearchButtonTitle: '从搜索历史中移除',
                  favoriteSearchesTitle: '收藏',
                  removeFavoriteSearchButtonTitle: '从收藏中移除'
                },
                errorScreen: {
                  titleText: '无法获取结果',
                  helpText: '你可能需要检查你的网络连接'
                },
                footer: {
                  selectText: '选择',
                  navigateText: '切换',
                  closeText: '关闭',
                  searchByText: '搜索提供者'
                },
                noResultsScreen: {
                  noResultsText: '无法找到相关结果',
                  suggestedQueryText: '你可以尝试查询',
                  reportMissingResultsText: '你认为该查询应该有结果？',
                  reportMissingResultsLinkText: '点击反馈'
                }
              }
            }
          }
        }
      }
    },

    footer: {
      message: 'Released under the Apache 2.0 License. Proud member of the Cat Alliance 🐱.',
      copyright: 'Copyright © 2024-present Nomagicln'
    }
  },

  locales: {
    root: {
      label: 'English',
      lang: 'en',
      themeConfig: {
        nav: [
          { text: 'Home', link: '/' },
          { text: 'Guide', link: '/guide/introduction' },
          { text: 'Upgrading', link: '/guide/upgrading' },
          { text: 'Reference', link: '/reference/cli' }
        ],
        sidebar: {
          '/guide/': [
            {
              text: 'Guide',
              items: [
                { text: 'Introduction', link: '/guide/introduction' },
                { text: 'Installation', link: '/guide/installation' },
                { text: 'Quick Start', link: '/guide/quick-start' },
                { text: 'Upgrading', link: '/guide/upgrading' }
              ]
            }
          ],
          '/reference/': [
            {
              text: 'Reference',
              items: [
                { text: 'CLI Commands', link: '/reference/cli' },
                { text: 'Architecture', link: '/reference/architecture' }
              ]
            }
          ]
        }
      }
    },
    zh: {
      label: '简体中文',
      lang: 'zh',
      link: '/zh/',
      title: "OpenBridge",
      description: "连接 AI 智能体与万物的通用桥梁",
      themeConfig: {
        nav: [
          { text: '首页', link: '/zh/' },
          { text: '指南', link: '/zh/guide/introduction' },
          { text: '升级', link: '/zh/guide/upgrading' },
          { text: '参考', link: '/zh/reference/cli' }
        ],
        sidebar: {
          '/zh/guide/': [
            {
              text: '指南',
              items: [
                { text: '介绍', link: '/zh/guide/introduction' },
                { text: '安装', link: '/zh/guide/installation' },
                { text: '快速开始', link: '/zh/guide/quick-start' },
                { text: '升级', link: '/zh/guide/upgrading' }
              ]
            }
          ],
          '/zh/reference/': [
            {
              text: '参考',
              items: [
                { text: 'CLI 命令', link: '/zh/reference/cli' },
                { text: '架构', link: '/zh/reference/architecture' }
              ]
            }
          ]
        },
        footer: {
          message: '基于 Apache 2.0 许可发布。Cat Alliance 成员 🐱。',
          copyright: 'Copyright © 2024-present Nomagicln'
        }
      }
    }
  }
})
