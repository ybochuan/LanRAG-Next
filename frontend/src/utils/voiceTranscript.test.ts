import assert from 'node:assert/strict'
import test from 'node:test'

import { cleanVoiceTranscript } from './voiceTranscript.ts'

test('strips the language and asr_text protocol prefix', () => {
  assert.equal(
    cleanVoiceTranscript('language English<asr_text>Hello. This is a voice input test.'),
    'Hello. This is a voice input test.',
  )
  assert.equal(cleanVoiceTranscript('Language: Chinese<asr_text>你好，世界。'), '你好，世界。')
  assert.equal(cleanVoiceTranscript('  language Russian <asr_text>  Привет  '), 'Привет')
})

test('keeps plain transcripts unchanged', () => {
  assert.equal(cleanVoiceTranscript('Hello world'), 'Hello world')
  assert.equal(cleanVoiceTranscript('  retain surrounding spacing?  '), 'retain surrounding spacing?')
})

test('does not strip a marker without a language prefix', () => {
  assert.equal(cleanVoiceTranscript('say <asr_text> literally'), 'say <asr_text> literally')
})
