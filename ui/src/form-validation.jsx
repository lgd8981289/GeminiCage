import React, { useEffect, useState } from "react";
import { createPortal } from "react-dom";

function validationMessage(field) {
  const state = field.validity;
  if (state.valueMissing) return "请填写或选择此项。";
  if (state.badInput) return "请输入有效的数值。";
  if (state.rangeUnderflow) return `数值不能小于 ${field.min}。`;
  if (state.rangeOverflow) return `数值不能大于 ${field.max}。`;
  if (state.tooShort) return `请至少输入 ${field.minLength} 个字符。`;
  if (state.tooLong) return `最多可输入 ${field.maxLength} 个字符。`;
  if (state.typeMismatch && field.type === "email") return "请输入完整的电子邮箱地址。";
  if (state.typeMismatch && field.type === "url") return "请输入完整的网址。";
  return "请检查此项的输入格式。";
}

// Keep HTML validation in charge of submission while rendering its explanation
// in the workbench language, including on a phone with an English system locale.
export function FormValidation() {
  const [notice, setNotice] = useState(null);
  useEffect(() => {
    let first;
    const invalid = event => {
      const field = event.target;
      if (!field.form || !field.validity || event.defaultPrevented) return;
      event.preventDefault();
      if (first) return;
      first = field;
      queueMicrotask(() => { first = null; });
      field.focus();
      setNotice({ field, form: field.form, text: validationMessage(field) });
    };
    const clearField = event => setNotice(current => current?.field === event.target ? null : current);
    const clearForm = event => setNotice(current => current?.form === event.target ? null : current);
    document.addEventListener("invalid", invalid, true);
    for (const name of ["input", "change"]) document.addEventListener(name, clearField);
    for (const name of ["submit", "reset"]) document.addEventListener(name, clearForm, true);
    return () => {
      document.removeEventListener("invalid", invalid, true);
      for (const name of ["input", "change"]) document.removeEventListener(name, clearField);
      for (const name of ["submit", "reset"]) document.removeEventListener(name, clearForm, true);
    };
  }, []);
  return notice?.form.isConnected ? createPortal(<p role="alert" className="error-message">{notice.text}</p>, notice.form) : null;
}
