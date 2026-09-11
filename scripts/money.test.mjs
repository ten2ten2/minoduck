import test from 'node:test'
import assert from 'node:assert/strict'
import { formatMoneyExact } from '../apps/console/app/utils/money.ts'

const digits = (value) => value.replace(/[^\d-]/g, '')

test('money formatting preserves 18 integer and 12 fractional digits', () => {
  const value = '999999999999999999.123456789123'
  const rendered = formatMoneyExact(value, 'USD', 'en-US')
  assert.equal(digits(rendered), '999999999999999999123456789123')
})

test('money formatting preserves tiny negative values', () => {
  const rendered = formatMoneyExact('-0.000000000001', 'USD', 'en-US')
  assert.equal(digits(rendered), '-0000000000001')
})

test('money formatting keeps at least two display decimals without inventing value', () => {
  assert.equal(digits(formatMoneyExact('1', 'USD', 'en-US')), '100')
  assert.equal(digits(formatMoneyExact('1.2000', 'USD', 'en-US')), '120')
})

test('money formatting uses the locale decimal separator while preserving digits', () => {
  const rendered = formatMoneyExact('1234.567890123456', 'EUR', 'de-DE')
  assert.match(rendered, /,/)
  assert.equal(digits(rendered), '1234567890123456')
})

test('money formatting does not reinterpret invalid ledger strings', () => {
  assert.equal(formatMoneyExact('1e3', 'USD', 'en-US'), '1e3')
  assert.equal(formatMoneyExact(null, 'USD', 'en-US'), '—')
})
