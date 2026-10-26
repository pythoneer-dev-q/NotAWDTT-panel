async function wdttReadJSONResponse(response) {
  const contentType = response.headers.get('content-type') || '';
  if (!/\bapplication\/(?:[a-z0-9.+-]*\+)?json\b/i.test(contentType)) {
    const location = response.redirected ? ' (переадресация: ' + response.url + ')' : '';
    throw new Error('API вернул страницу вместо JSON: HTTP ' + response.status + location + '. Проверьте адрес панели и войдите заново.');
  }
  try {
    return await response.json();
  } catch (error) {
    throw new Error('API вернул повреждённый JSON: ' + response.url);
  }
}

// Axios otherwise treats a redirected login page as a successful API response.
if (typeof axios !== 'undefined') {
  axios.interceptors.response.use((response) => {
    const url = response.config && response.config.url || '';
    const contentType = response.headers && response.headers['content-type'] || '';
    if (/\b(?:panel\/)?api\//.test(url) &&
        (!/\bapplication\/(?:[a-z0-9.+-]*\+)?json\b/i.test(contentType) ||
         typeof response.data === 'string' && /^\s*</.test(response.data))) {
      return Promise.reject(new Error('API вернул страницу вместо JSON: HTTP ' + response.status + '. Проверьте адрес панели и войдите заново.'));
    }
    return response;
  });
}
