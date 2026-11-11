import { expect, test } from '@playwright/test'
import { addCard, createBoard, login, newUserPage } from './helpers'

test('create a board, add cards and drag between lists', async ({ page }) => {
  await login(page, 'Alice')
  await createBoard(page, ['To do', 'Doing'])
  await addCard(page, 'To do', 'First card')
  await addCard(page, 'To do', 'Second card')

  const todo = page.getByRole('region', { name: 'List To do' })
  const doing = page.getByRole('region', { name: 'List Doing' })
  await todo.getByRole('button', { name: 'Open card First card' }).dragTo(doing.locator('[data-list-id]').last())
  await expect(doing.getByRole('button', { name: 'Open card First card' })).toBeVisible()
  await expect(todo.getByRole('button', { name: 'Open card First card' })).toHaveCount(0)

  // The move was saved: it survives a reload.
  await page.reload()
  await expect(page.getByRole('region', { name: 'List Doing' }).getByRole('button', { name: 'Open card First card' })).toBeVisible()
})

test('card detail: description, checklist and comments', async ({ page }) => {
  await login(page, 'Alice')
  await createBoard(page, ['Backlog'])
  await addCard(page, 'Backlog', 'Detailed card')
  await page.getByRole('button', { name: 'Open card Detailed card' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: 'Add a more detailed description…' }).click()
  await dialog.getByLabel('Description').fill('Some **bold** text')
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog.locator('strong', { hasText: 'bold' })).toBeVisible()

  await dialog.getByRole('button', { name: 'Checklist', exact: true }).click()
  await page.getByLabel('Checklist title').fill('Steps')
  await page.getByRole('button', { name: 'Add checklist' }).click()
  await dialog.getByLabel('Add item to Steps').fill('Do it')
  await dialog.getByLabel('Add item to Steps').press('Enter')
  await dialog.getByRole('checkbox', { name: 'Do it' }).check()
  await expect(dialog.getByText('100%')).toBeVisible()

  await dialog.getByLabel('New comment').fill('Looks good')
  await dialog.getByRole('button', { name: 'Comment', exact: true }).click()
  await expect(dialog.getByTestId('comment').filter({ hasText: 'Looks good' })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('button', { name: 'Open card Detailed card' })).toContainText('1/1')
})

test('two browsers see each other’s changes live', async ({ browser }) => {
  const alice = await newUserPage(browser, 'Alice')
  const { ws, url } = await createBoard(alice, ['Shared'])
  // Invite Bob by email; he already exists from earlier sign-ins.
  const bob = await newUserPage(browser, 'Bob')
  await alice.getByRole('link', { name: 'Workspace', exact: true }).click()
  await alice.getByRole('tab', { name: 'members' }).click()
  await alice.getByPlaceholder('name@example.com').fill('bob@example.com')
  await alice.getByRole('button', { name: 'Invite' }).click()
  await expect(alice.getByText('bob@example.com').first()).toBeVisible()
  await alice.goto(url)
  await bob.goto(url)
  await expect(bob.getByTestId('live-indicator')).toHaveClass(/bg-emerald/)
  await expect(alice.getByTestId('live-indicator')).toHaveClass(/bg-emerald/)

  await addCard(alice, 'Shared', 'From Alice')
  await expect(bob.getByRole('button', { name: 'Open card From Alice' })).toBeVisible()
  await addCard(bob, 'Shared', 'From Bob')
  await expect(alice.getByRole('button', { name: 'Open card From Bob' })).toBeVisible()
  // Bob was told he joined the workspace.
  await expect(bob.getByTestId('unread-count')).toBeVisible()
  void ws
})

test('search finds a card', async ({ page }) => {
  await login(page, 'Alice')
  await createBoard(page, ['Ideas'])
  const title = `Zebra crossing ${Date.now()}`
  await addCard(page, 'Ideas', title)
  await page.getByLabel('Search cards').fill('zebra')
  await page.getByLabel('Search cards').press('Enter')
  await expect(page.getByRole('link', { name: new RegExp(title) })).toBeVisible()
})

test('attachments upload and show on the card', async ({ page }) => {
  test.skip(!process.env.KOWHAI_E2E_S3, 'needs an S3 store (set KOWHAI_E2E_S3=1)')
  await login(page, 'Alice')
  await createBoard(page, ['Files'])
  await addCard(page, 'Files', 'With file')
  await page.getByRole('button', { name: 'Open card With file' }).click()
  await page.getByTestId('file-input').setInputFiles({ name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') })
  await expect(page.getByRole('link', { name: 'notes.txt' })).toBeVisible()
})
