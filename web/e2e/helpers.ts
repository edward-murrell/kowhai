import { expect, type Browser, type Page } from '@playwright/test'

// login signs in through the built-in dev provider by picking a user.
export async function login(page: Page, name: string) {
  await page.goto('/auth/login?return_to=/')
  await page.getByRole('button', { name: new RegExp(name) }).click()
  await expect(page.getByRole('button', { name: 'Account menu' })).toBeVisible()
}

export async function newUserPage(browser: Browser, name: string) {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await login(page, name)
  return page
}

export function unique(prefix: string) {
  return `${prefix} ${Date.now().toString(36)}${Math.floor(Math.random() * 1000)}`
}

// createBoard makes a workspace and a board with lists, through the UI.
export async function createBoard(page: Page, lists: string[]) {
  const ws = unique('Team')
  await page.goto('/')
  await page.getByRole('button', { name: 'New workspace' }).click()
  await page.getByPlaceholder('e.g. Product team').fill(ws)
  await page.getByRole('button', { name: 'Create', exact: true }).click()
  await expect(page.getByRole('heading', { name: ws })).toBeVisible()
  await page.getByRole('button', { name: 'New board' }).click()
  const board = unique('Board')
  await page.getByRole('dialog').getByRole('textbox').first().fill(board)
  await page.getByRole('button', { name: 'Create board' }).click()
  await expect(page.getByRole('heading', { name: board })).toBeVisible()
  for (const name of lists) {
    const add = page.getByRole('button', { name: '+ Add a list' })
    if (await add.isVisible()) await add.click()
    await page.getByLabel('New list name').fill(name)
    await page.getByRole('button', { name: 'Add list' }).click()
    await expect(page.getByRole('region', { name: `List ${name}` })).toBeVisible()
  }
  return { ws, board, url: page.url() }
}

export async function addCard(page: Page, list: string, title: string) {
  const col = page.getByRole('region', { name: `List ${list}` })
  const add = col.getByRole('button', { name: '+ Add a card' })
  if (await add.isVisible()) await add.click()
  await col.getByLabel('New card title').fill(title)
  await col.getByLabel('New card title').press('Enter')
  await expect(col.getByRole('button', { name: `Open card ${title}` })).toBeVisible()
}
